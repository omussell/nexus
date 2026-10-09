// Package ingest runs the Nauvis pipeline: for every input file it extracts
// the decompressed .json (byte-for-byte unchanged), records the DOIs of its
// items in the database, mints CROIDs via the CROID service, and optionally
// writes Parquet files for each DOI. Files are processed in parallel across a
// pool of workers, sharing a single SQLite writer so the database stays
// consistent.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/nexus/croid/client"
	"github.com/nexus/nauvis/internal/extract"
	"github.com/nexus/nauvis/internal/parquet"
	"github.com/nexus/nauvis/internal/store"
)

// Config holds optional configuration for the ingest pipeline.
type Config struct {
	CROIDURL  string // CROID service URL (e.g. "http://localhost:9090")
	ParquetDir string // directory to write Parquet files ("" to disable)
	IgnoreDupes bool  // skip duplicate DOI logging
}

// Run processes every input file in inDir in parallel, writing decompressed
// .json files to outDir and recording their items in the store. It returns the
// number of files processed, the number that failed, and any aggregate error.
//
// If cfg.ParquetDir is non-empty, each DOI's JSON is converted to a Parquet
// file named {croid}.parquet in that directory after minting a CROID.
// If cfg.CROIDURL is non-empty, CROIDs are minted for each DOI.
func Run(ctx context.Context, inDir, outDir string, st *store.Store, jobs int, lg *slog.Logger, cfg Config) (ok, failed int, err error) {
	if lg == nil {
		lg = slog.Default()
	}
	files, err := extract.Files(inDir)
	if err != nil {
		return 0, 0, err
	}
	if len(files) == 0 {
		return 0, 0, nil
	}

	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}
	if jobs < 1 {
		jobs = 1
	}
	if jobs > len(files) {
		jobs = len(files)
	}

	// Build CROID client if URL is provided.
	var cClient *client.Client
	if cfg.CROIDURL != "" {
		cClient = client.New(cfg.CROIDURL)
	}

	// Prepare parquet directory if configured.
	var parquetMu sync.Mutex
	if cfg.ParquetDir != "" {
		if err := os.MkdirAll(cfg.ParquetDir, 0o755); err != nil {
			return 0, 0, fmt.Errorf("create parquet dir: %w", err)
		}
	}

	work := make(chan string)
	results := make(chan fileResult, len(files))

	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for inPath := range work {
				results <- processOne(ctx, inPath, outDir, st, cClient, cfg.ParquetDir, &parquetMu, cfg.IgnoreDupes, lg)
			}
		}()
	}

	go func() {
		defer close(work)
		for _, p := range files {
			select {
			case work <- p:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.err != nil {
			failed++
			lg.Error("file: "+res.name+": "+res.err.Error())
			continue
		}
		if !cfg.IgnoreDupes {
			for _, d := range res.dupes {
				lg.Error("duplicate DOI "+d.Doi+" in "+d.File+" (already recorded elsewhere; skipping)")
			}
		}
		ok++
		lg.Info("file: "+res.name+" -> "+res.outPath, "items", res.items, "parquet", res.parquet)
	}

	if failed > 0 {
		err = fmt.Errorf("%d of %d input files failed", failed, len(files))
	}
	return ok, failed, err
}

type fileResult struct {
	name     string
	outPath  string
	items    int
	parquet  int
	dupes    []store.Duplicate
	err      error
}

// processOne runs the full pipeline for a single input file.
func processOne(ctx context.Context, inPath, outDir string, st *store.Store,
	cClient *client.Client, parquetDir string, parquetMu *sync.Mutex, ignoreDupes bool, lg *slog.Logger,
) fileResult {
	name := extract.BaseName(inPath)
	outPath := filepath.Join(outDir, name)

	dois, items, _, err := extract.Process(inPath, outPath)
	if err != nil {
		return fileResult{name: name, outPath: outPath, err: err}
	}

	dupes, inserted, err := st.RecordMany(ctx, outPath, dois)
	if err != nil {
		return fileResult{name: name, outPath: outPath, items: len(dois), dupes: dupes, err: err}
	}

	// Skip CROID + Parquet work for duplicate items only.
	var validDois []string
	var validItems []json.RawMessage
	for i, doi := range dois {
		isDupe := false
		for _, d := range dupes {
			if d.Doi == doi {
				isDupe = true
				break
			}
		}
		if !isDupe {
			validDois = append(validDois, doi)
			validItems = append(validItems, items[i])
		}
	}

	if len(validDois) == 0 {
		return fileResult{name: name, outPath: outPath, items: inserted, dupes: dupes}
	}

	parquetCount := 0
	for i, doi := range validDois {
		// Mint CROID
		var croid string
		if cClient != nil {
			var cerr error
			croid, cerr = cClient.Mint(ctx, doi)
			if cerr != nil {
				lg.Error("croid mint: "+cerr.Error()+" for "+doi)
				continue
			}
		}

		// Write Parquet file
		if parquetDir != "" && croid != "" {
			parquetMu.Lock()
			pb, perr := parquet.Convert(validItems[i])
			if perr == nil {
				pPath := filepath.Join(parquetDir, croid+".parquet")
				if werr := atomicWrite(pPath, pb, 0o644); werr == nil {
					parquetCount++
				} else {
					lg.Error("write parquet: "+werr.Error()+" for "+doi)
				}
			} else {
				lg.Error("convert parquet: "+perr.Error()+" for "+doi)
			}
			parquetMu.Unlock()
		}
	}

	return fileResult{name: name, outPath: outPath, items: inserted, parquet: parquetCount, dupes: dupes}
}

// atomicWrite writes data to path, creating the parent directory if needed,
// writing to a temp file in the same directory, then renaming into place.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
