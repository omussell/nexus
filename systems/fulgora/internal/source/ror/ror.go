// Package ror implements the Fulgora source for the ROR (Research
// Organization Registry) data dump, served by the ror-data Zenodo community.
//
// The dump is refreshed at least monthly. CheckLatest asks the Zenodo API for
// the newest record in the community and the newest file attached to it;
// Process unpacks the downloaded zip into a versioned output directory.
//
// See the Zenodo community API:
//
//	https://zenodo.org/api/communities/ror-data/records?q=&sort=newest
//
// and the ROR data documentation:
//
//	https://ror.readme.io/docs/data-dump
package ror

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexus/fulgora/internal/source"
)

const (
	name      = "ror"
	apiBase   = "https://zenodo.org"
	community = "ror-data"
)

// Source fetches and standardizes the ROR data dump.
type Source struct {
	apiBase string // overridable in tests
	client  *http.Client
}

// New returns a ROR source.
func New() *Source {
	return &Source{apiBase: apiBase, client: newClient(30 * time.Second)}
}

// NewWithAPIBase returns a ROR source whose API base is base (testing hook).
func NewWithAPIBase(base string) *Source {
	return &Source{apiBase: base, client: newClient(30 * time.Second)}
}

func newClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// zenodoFile is one file entry in a Zenodo record.
type zenodoFile struct {
	Key   string `json:"key"`
	Links struct {
		Self     string `json:"self"`
		Download string `json:"download"`
	} `json:"links"`
}

// downloadLink returns the raw-byte URL for this file. The community-records
// view only exposes links.self (which already ends in /content); the record
// view exposes links.download. Prefer download when present.
func (f zenodoFile) downloadLink() string {
	if f.Links.Download != "" {
		return f.Links.Download
	}
	if f.Links.Self != "" {
		if strings.HasSuffix(f.Links.Self, "/content") {
			return f.Links.Self
		}
		return f.Links.Self + "/content"
	}
	return ""
}

// zenodoRecord is the subset of the Zenodo community-records hit we need.
type zenodoRecord struct {
	Files []zenodoFile `json:"files"`
}

// CheckLatest queries the Zenodo community API and returns the newest record's
// newest file.
func (s *Source) CheckLatest(ctx context.Context) (*source.Latest, error) {
	u := s.apiBase + "/api/communities/" + community + "/records?" +
		url.Values{"q": {}, "sort": {"newest"}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ror: query zenodo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ror: query zenodo: unexpected status %s", resp.Status)
	}

	var envelope struct {
		Hits struct {
			Hits []zenodoRecord `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("ror: decode zenodo response: %w", err)
	}
	if len(envelope.Hits.Hits) == 0 {
		return nil, fmt.Errorf("ror: no records found in zenodo community %q", community)
	}

	rec := envelope.Hits.Hits[0] // newest record
	if len(rec.Files) == 0 {
		return nil, fmt.Errorf("ror: record %d has no files", len(rec.Files))
	}
	file := rec.Files[len(rec.Files)-1] // newest file attached to the record
	if dl := file.downloadLink(); dl == "" {
		return nil, fmt.Errorf("ror: file %q has no download link", file.Key)
	}

	return &source.Latest{
		Version:  versionFromFilename(file.Key),
		URL:      file.downloadLink(),
		Filename: file.Key,
	}, nil
}

// Process unpacks the raw zip at rawPath into a versioned output directory
// under outRoot and returns the output directory.
func (s *Source) Process(ctx context.Context, version, rawPath, outRoot string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("ror: empty version")
	}
	outDir := filepath.Join(outRoot, version)
	if err := unpackZip(rawPath, outDir); err != nil {
		return "", err
	}
	return outDir, nil
}

// Name implements source.Source.
func (s *Source) Name() string { return name }

// versionFromFilename extracts the version label from a ROR dump filename.
// "v1.34-2023-10-12-ror-data.zip" -> "1.34". Only a known dump extension
// (.zip/.xml/.csv) is stripped so plain labels like "1.63" survive intact.
func versionFromFilename(name string) string {
	base := filepath.Base(name)
	for _, ext := range []string{".zip", ".xml", ".csv"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	base = strings.TrimPrefix(base, "v")
	if i := strings.Index(base, "-"); i > 0 {
		return base[:i]
	}
	return base
}

// unpackZip extracts every entry of zipPath into outDir, refusing entries
// that would escape the target (zip-slip).
func unpackZip(zipPath, outDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("ror: open zip %s: %w", zipPath, err)
	}
	defer r.Close()

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, f := range r.File {
		target, err := safeJoin(outDir, f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func safeJoin(outDir, name string) (string, error) {
	target := filepath.Join(outDir, name)
	rel, err := filepath.Rel(outDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("ror: unsafe zip entry %q", name)
	}
	return target, nil
}

func writeEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	dst, err := os.Create(target)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, rc); err != nil {
		return err
	}
	return dst.Close()
}
