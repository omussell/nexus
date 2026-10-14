// Package source defines the contract a third-party dataset source must
// satisfy and provides shared helpers for downloading files.
//
// # Adding a New Data Source
//
// To add a new data source, follow these steps:
//
//  1. Create a package under internal/source/<name>/ with a Go file.
//
//  2. Define a struct and implement the Source interface below. At minimum:
//
//     - Name() string — returns the source identifier (also used as the
//       directory name under the data root and the key in the database).
//
//     - CheckLatest(ctx) (*Latest, error) — queries the upstream provider
//       (HTTP API, git repo, etc.) and returns the newest version label
//       and download URL. The version must change whenever new data arrives
//       so the skip/dedup logic in the collector can detect updates.
//
//     - Process(ctx, version, rawPath, outRoot) (string, error) — takes
//       the raw downloaded file and produces a clean, versioned JSON output
//       under outRoot. Use the output.JSONFile helper (from internal/output)
//       to write NDJSON atomically (temp file + rename). Return the path to
//       the output file.
//
//     - ConvertParquet(data []byte) ([]byte, error) — converts the NDJSON
//       output from Process() into Parquet bytes. Define a row struct with
//       parquet tags (see the ROR or Retraction Watch sources for examples).
//       Return nil if the source should not produce Parquet output.
//
//     Example skeleton:
//
//       package mysource
//
//       import (
//           "context"
//           "github.com/nexus/fulgora/internal/output"
//           "github.com/nexus/fulgora/internal/source"
//       )
//
//       const name = "mysource"
//
//       type Source struct {
//           client *http.Client
//       }
//
//       func New() *Source { return &Source{client: &http.Client{Timeout: 30 * time.Second}} }
//
//       func (s *Source) Name() string { return name }
//       func (s *Source) CheckLatest(ctx context.Context) (*source.Latest, error) { ... }
//       func (s *Source) Process(ctx context.Context, version, rawPath, outRoot string) (string, error) {
//           // Parse rawPath → transform → output.JSONFile(rows, outRoot, name+"-"+version)
//       }
//       func (s *Source) ConvertParquet(data []byte) ([]byte, error) { ... }
//
//  3. Register the source by adding one entry to the registry map in
//     internal/sources/sources.go:
//
//     "mysource": func() source.Source { return mysource.New() },
//
//  4. Run go test ./... to verify the build passes. Optional: add a test
//     file in the same package that exercises CheckLatest and Process with
//     httptest or inline fixtures.
//
// See internal/source/ror/, internal/source/retractionwatch/, and
// internal/source/openfunderregistry/ for complete examples.
package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Latest describes the newest version of a source's dataset and where to get
// it.
type Latest struct {
	// Version is the source's own version label (e.g. "1.63", "2026-08-15").
	// It must change whenever the upstream data is updated, so that the
	// collector's dedup logic can detect new versions.
	Version string
	// URL is where the raw file is downloaded from.
	URL string
	// Filename is the raw file's name (used for the initial_input path).
	Filename string
}

// Source is a third-party dataset: ROR, Retraction Watch, Open Funder Registry, etc.
// Each source is implemented in its own package under internal/source/<name>
// and registered in the sources package.
type Source interface {
	// Name returns the source identifier, e.g. "ror" or "retractionwatch".
	// It is used as the per-source directory name and the "source" key in the
	// database.
	Name() string

	// CheckLatest queries the source and reports its newest available version
	// and download URL, without downloading the file itself. If the source has
	// no explicit versioning (e.g. an in-place-updated CSV), derive the version
	// from a git commit date or the current date so that the collector's skip
	// logic can still detect changes.
	CheckLatest(ctx context.Context) (*Latest, error)

	// Process takes the raw downloaded file (rawPath) and the version label
	// (version) and produces the standardized output under outRoot (e.g.
	// unpacking an archive into a versioned directory), returning the final
	// output path (directory or file) to record in the database.
	//
	// Use output.JSONFile to write the result as NDJSON. The file is written
	// atomically (temp + rename) so a failure never leaves partial content.
	Process(ctx context.Context, version, rawPath, outRoot string) (string, error)

	// ConvertParquet converts the source's NDJSON output (as written by
	// Process) into Parquet bytes. Define a row struct with parquet tags
	// (see internal/source/ror for an example) and return the encoded bytes,
	// or nil if Parquet output is not needed for this source.
	ConvertParquet(data []byte) ([]byte, error)
}

// Download fetches url to dest. It writes to a temporary file in the same
// directory and renames into place, so a failed or interrupted download never
// leaves a partial dest behind.
func Download(ctx context.Context, client *http.Client, url, dest string) error {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("source: download %s: unexpected status %s", url, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	n, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("source: download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("source: download %s: empty body", url)
	}

	return os.Rename(tmpName, dest)
}
