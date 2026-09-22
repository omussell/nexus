// Package ingest runs the Nauvis pipeline: for every input file it extracts
// the decompressed .json (byte-for-byte unchanged), records the DOIs of its
// items in the database, and reports any DOIs that appear in more than one
// file as errors (skipping them). Files are processed in parallel across a
// pool of workers, sharing a single SQLite writer so the database stays
// consistent.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/nexus/croid/client"
	"github.com/nexus/nauvis/internal/extract"
	"github.com/nexus/nauvis/internal/store"
)

// Run processes every input file in inDir in parallel, writing decompressed
// .json files to outDir and recording their items in the store. It returns the
// number of files processed, the number that failed, and any aggregate error.
//
// If croidClient is non-nil, a CROID is minted for each newly inserted DOI
// in a background goroutine. Errors from minting are logged but do not
// affect processing.
func Run(ctx context.Context, inDir, outDir string, st *store.Store, jobs int, lg *slog.Logger, croidClient *client.Client) (ok, failed int, err error) {
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

	work := make(chan string)
	results := make(chan fileResult, len(files))

	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
	go func() {
		defer wg.Done()
		for inPath := range work {
			results <- processOne(ctx, inPath, outDir, st, croidClient, lg)
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
		for _, d := range res.dupes {
			lg.Error("duplicate DOI "+d.Doi+" in "+d.File+" (already recorded elsewhere; skipping)")
		}
		ok++
		lg.Info("file: "+res.name+" -> "+res.outPath, "items", res.items)
	}

	if failed > 0 {
		err = fmt.Errorf("%d of %d input files failed", failed, len(files))
	}
	return ok, failed, err
}

type fileResult struct {
	name    string
	outPath string
	items   int
	dupes   []store.Duplicate
	err     error
}

// processOne runs the full pipeline for a single input file. If croidClient
// is non-nil, newly inserted DOIs are minted as CROIDs with their record in
// background goroutines.
func processOne(ctx context.Context, inPath, outDir string, st *store.Store, croidClient *client.Client, lg *slog.Logger) fileResult {
	name := extract.BaseName(inPath)
	outPath := filepath.Join(outDir, name)

	dois, items, _, err := extract.Process(inPath, outPath)
	if err != nil {
		return fileResult{name: name, outPath: outPath, err: err}
	}

	dupes, insertedDois, inserted, err := st.RecordMany(ctx, outPath, dois)
	if err != nil {
		return fileResult{name: name, outPath: outPath, items: len(dois), dupes: dupes, err: err}
	}

	// Build a DOI-to-record index for this file so we can look up the record
	// by DOI when minting CROIDs.
	doiIdx := make(map[string]json.RawMessage, len(items))
	for i, item := range items {
		if i < len(dois) {
			doiIdx[dois[i]] = item
		}
	}

	// Mint CROIDs for newly inserted DOIs in background goroutines.
	if croidClient != nil && len(insertedDois) > 0 {
		var wg sync.WaitGroup
		wg.Add(len(insertedDois))
		for _, doi := range insertedDois {
			go func(doi string) {
				defer wg.Done()
				record := doiIdx[doi]
				if _, err := croidClient.Mint(ctx, "DOI", doi, "nauvis", string(record)); err != nil {
					lg.Warn("croid: mint", "doi", doi, "err", err)
				}
			}(doi)
		}
		wg.Wait()
	}

	return fileResult{name: name, outPath: outPath, items: inserted, dupes: dupes}
}
