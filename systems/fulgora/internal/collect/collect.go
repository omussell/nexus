// Package collect runs the fetch workflow for sources: check for new
// versions, skip if already collected, download the raw file, process it,
// record in the database, mint CROIDs, and optionally write Parquet files.
//
// # Collection Pipeline
//
// Run (or RunFiltered) launches one goroutine per source name. Each goroutine
// calls Collect, which follows these steps:
//
//  1. CheckLatest — ask the source what its newest version is. If the call
//     fails (network issue, API down), fall back to the latest version on disk
//     so the skip path is used and no download is attempted.
//
//  2. Skip dedup — if the exact (source, version) is already in the database,
//     return immediately without downloading.
//
//  3. Download — fetch the raw file to rawRoot/<source>/initial_input/.
//     Written atomically (temp + rename) so a partial file is never left behind.
//
//  4. Process — call src.Process(version, rawPath, outRoot). The source
//     parses the raw file and writes a clean NDJSON output. The path is
//     recorded in the database.
//
//  5. Record — insert the dataset row and upsert the latest pointer in
//     SQLite.
//
//  6. Parquet — if parquetDir is non-empty, read the NDJSON output and call
//     src.ConvertParquet(data). The source defines its own schema via a row
//     struct with parquet tags. The resulting Parquet file is written to both
//     the consolidated output directory and (if croidClient is configured)
//     a CROID-named file.
//
//  7. CROID — if croidClient is non-nil, mint a CROID for the dataset and
//     write a CROID-named Parquet file.
//
// # Adding a New Source
//
// New sources need no changes to this file. See internal/source/source.go for
// the full guide, but the short version is: implement the Source interface in
// a package under internal/source/<name>/ and register it in internal/sources.
// The Collect function calls src.Process and src.ConvertParquet generically —
// the Parquet schema lives entirely inside the source package.
package collect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nexus/croid/client"
	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/sources"
	"github.com/nexus/fulgora/internal/store"
)

// Result is the outcome of one Collect for a single source+version.
type Result struct {
	Source       string
	Version      string
	CollectedAt  string
	InitialInput string
	Output       string
	Croid        string
	Skipped      bool // true when the version was already recorded in the database
}

// Run fetches the latest version of all sources and records them in the store.
// It returns a slice of results, one per source.
//
// If parquetDir is non-empty, a Parquet file is written for each newly
// collected dataset, named {croid}.parquet in that directory.
// If croidClient is non-nil, a CROID is minted for each newly recorded dataset.
func Run(ctx context.Context, s *store.Store, rawRoot, parquetDir string, croidClient *client.Client) ([]Result, error) {
	return RunFiltered(ctx, s, rawRoot, parquetDir, sources.All(), croidClient)
}

// RunFiltered fetches the latest version of the specified sources and records
// them in the store. It returns a slice of results, one per source.
//
// If parquetDir is non-empty, a Parquet file is written for each newly
// collected dataset, named {croid}.parquet in that directory.
// If croidClient is non-nil, a CROID is minted for each newly recorded dataset.
func RunFiltered(ctx context.Context, s *store.Store, rawRoot, parquetDir string, srcNames []string, croidClient *client.Client) ([]Result, error) {

	if parquetDir != "" {
		if err := os.MkdirAll(parquetDir, 0o755); err != nil {
			return nil, fmt.Errorf("collect: create parquet dir: %w", err)
		}
	}

	results := make([]Result, len(srcNames))
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for i, name := range srcNames {
		i := i
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			src, err := sources.Get(name)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			res, err := Collect(ctx, src, s, rawRoot, parquetDir, croidClient)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			results[i] = *res
			mu.Unlock()
		}()
	}
	wg.Wait()

	return results, firstErr
}

// Collect fetches (or reuses) the latest known version of src and records it
// in the store. rawRoot is the root under which per-source directories live,
// e.g. "systems/fulgora"; the raw file is stored under rawRoot/<source>/
// initial_input and the standardized output under rawRoot/<source>/output.
//
// If croidClient is non-nil, a CROID is minted for the newly recorded dataset.
// If parquetDir is non-empty, a Parquet file is written for the dataset.
func Collect(ctx context.Context, src source.Source, s *store.Store, rawRoot, parquetDir string, croidClient *client.Client) (*Result, error) {
	latest, err := src.CheckLatest(ctx)
	if err != nil {
		// If CheckLatest fails (network issue, API down, etc.), fall back to the
		// latest version we already have on disk so that the skip path is used
		// and no download is needed.
		if rec, getErr := s.Latest(ctx, src.Name()); getErr == nil {
			return &Result{
				Source:      rec.Source,
				Version:     rec.Version,
				CollectedAt: rec.CollectedAt,
				Output:      rec.Output,
				Skipped:     true,
			}, nil
		}
		// Nothing on disk — this is a real error.
		return nil, fmt.Errorf("collect: %s: %w", src.Name(), err)
	}
	if latest.Version == "" {
		return nil, fmt.Errorf("collect: %s: source returned no version", src.Name())
	}

	// If we already collected this exact version, do not download it again.
	if rec, err := s.Get(ctx, src.Name(), latest.Version); err == nil {
		return &Result{
			Source:       rec.Source,
			Version:      rec.Version,
			CollectedAt:  rec.CollectedAt.UTC().Format(time.RFC3339),
			InitialInput: rec.InitialInput,
			Output:       rec.Output,
			Croid:        rec.Croid,
			Skipped:      true,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := time.Now().UTC()

	srcDir := filepath.Join(rawRoot, src.Name())
	inputPath := filepath.Join(
		srcDir, "initial_input",
		fmt.Sprintf("%s-%s_%s", src.Name(), latest.Version, stamp(now))+extOf(latest.Filename, ".zip"),
	)
	outputRoot := filepath.Join(srcDir, "output")

	if err := source.Download(ctx, nil, latest.URL, inputPath); err != nil {
		os.Remove(inputPath) // never leave a partial file behind
		return nil, fmt.Errorf("collect: %s: download %s: %w", src.Name(), latest.Version, err)
	}

	outPath, err := src.Process(ctx, latest.Version, inputPath, outputRoot)
	if err != nil {
		return nil, fmt.Errorf("collect: %s: process %s: %w", src.Name(), latest.Version, err)
	}

	rec, err := s.Record(ctx, store.Dataset{
		Source:       src.Name(),
		Version:      latest.Version,
		CollectedAt:  now,
		InitialInput: inputPath,
		Output:       outPath,
	})
	if err != nil {
		return nil, fmt.Errorf("collect: %s: record %s: %w", src.Name(), latest.Version, err)
	}

	// Write consolidated Parquet file alongside the JSON output.
	if parquetDir != "" {
		pb, err := convertForSource(src, outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "collect: %s: convert parquet %s: %v\n", src.Name(), outPath, err)
		} else if pb != nil {
			if _, err := writeParquetBytes(pb, rawRoot, strings.TrimSuffix(filepath.Base(outPath), filepath.Ext(outPath))+".parquet"); err != nil {
				fmt.Fprintf(os.Stderr, "collect: %s: write consolidated parquet %s: %v\n", src.Name(), outPath, err)
			}
		}
	}

	// Mint a CROID and write CROID-named Parquet for the newly recorded dataset.
	if croidClient != nil && rec.ID > 0 {
		croid, mintErr := croidClient.MintWith(ctx, "dataset", fmt.Sprintf("%s:%s", src.Name(), latest.Version))
		if mintErr != nil {
			return nil, fmt.Errorf("collect: %s: mint croid %s: %w", src.Name(), latest.Version, mintErr)
		}
		rec.Croid = croid
		if err := s.UpdateCroid(ctx, rec.ID, croid); err != nil {
			return nil, fmt.Errorf("collect: %s: update croid %s: %w", src.Name(), latest.Version, err)
		}

		// Write CROID-named Parquet file if directory is configured.
		if parquetDir != "" {
			pb, err := convertForSource(src, outPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "collect: %s: convert parquet %s: %v\n", src.Name(), outPath, err)
			} else if pb != nil {
				if _, err := writeParquetBytes(pb, parquetDir, croid+".parquet"); err != nil {
					fmt.Fprintf(os.Stderr, "collect: %s: write parquet %s: %v\n", src.Name(), croid, err)
				}
			}
		}
	}

	return &Result{
		Source:       rec.Source,
		Version:      rec.Version,
		CollectedAt:  rec.CollectedAt.UTC().Format(time.RFC3339),
		InitialInput: rec.InitialInput,
		Output:       rec.Output,
		Croid:        rec.Croid,
		Skipped:      false,
	}, nil
}

// convertForSource reads the output file at outputPath and converts it to
// Parquet bytes using the source's own ConvertParquet implementation.
func convertForSource(src source.Source, outputPath string) ([]byte, error) {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("parquet: read %s: %w", outputPath, err)
	}
	return src.ConvertParquet(data)
}

// writeParquetBytes atomically writes parquetBytes to destPath (created under
// destDir), using a temp-and-rename pattern so a failed write never leaves
// a partial file behind.
func writeParquetBytes(parquetData []byte, destDir, destName string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	pPath := filepath.Join(destDir, destName)
	tmp := pPath + ".tmp"
	if err := os.WriteFile(tmp, parquetData, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, pPath); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return pPath, nil
}

func stamp(t time.Time) string {
	// stamp is a sortable filesystem-safe timestamp for the initial_input filename
	// (e.g. "2025-01-01T00-00-00Z"), matching the examples in the system README.
	return t.Format("2006-01-02T15-04-05Z")
}

// extOf returns the extension of name, or fallback if name has none.
func extOf(name, fallback string) string {
	if e := filepath.Ext(name); e != "" {
		return e
	}
	return fallback
}
