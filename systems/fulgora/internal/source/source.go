// Package source defines the contract a third-party dataset source must
// satisfy and provides a shared download helper.
//
// Adding a new source: create a package under internal/source/<name> that
// implements Source, then register it in internal/sources.
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
	Version string
	// URL is where the raw file is downloaded from.
	URL string
	// Filename is the raw file's name (used for the initial_input path).
	Filename string
}

// Source is a third-party dataset: ROR, Retraction Watch, ...
type Source interface {
	// Name returns the source identifier, e.g. "ror" or "retractionwatch".
	// It is used as the per-source directory name and the "source" key in the
	// database.
	Name() string

	// CheckLatest queries the source and reports its newest available version
	// and download URL, without downloading the file itself.
	CheckLatest(ctx context.Context) (*Latest, error)

	// Process takes the raw downloaded file (rawPath) and the version label
	// (version) and produces the standardized output under outRoot (e.g.
	// unpacking an archive into a versioned directory), returning the final
	// output path (directory or file) to record in the database.
	Process(ctx context.Context, version, rawPath, outRoot string) (string, error)
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
