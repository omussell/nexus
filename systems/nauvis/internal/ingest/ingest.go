// Package ingest runs the Nauvis pipeline: for every input file it extracts
// the decompressed .json (byte-for-byte unchanged), records the DOIs of its
// items in the database, and reports any DOIs that appear in more than one
// file as errors (skipping them). Files are processed in parallel across a
// pool of workers, sharing a single SQLite writer so the database stays
// consistent.
package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/nexus/nauvis/internal/extract"
	"github.com/nexus/nauvis/internal/store"
)

// Run processes every input file in inDir in parallel, writing decompressed
// .json files to outDir and recording their items in the store. It returns the
// number of files processed, the number that failed, and any aggregate error.
func Run(ctx context.Context, inDir, outDir string, st *store.Store, jobs int, lg *slog.Logger) (ok, failed int, err error) {
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
				results <- processOne(ctx, inPath, outDir, st)
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

// processOne runs the full pipeline for a single input file.
func processOne(ctx context.Context, inPath, outDir string, st *store.Store) fileResult {
	name := extract.BaseName(inPath)
	outPath := filepath.Join(outDir, name)

	dois, _, err := extract.Process(inPath, outPath)
	if err != nil {
		return fileResult{name: name, outPath: outPath, err: err}
	}

	dupes, inserted, err := st.RecordMany(ctx, outPath, dois)
	if err != nil {
		return fileResult{name: name, outPath: outPath, items: len(dois), dupes: dupes, err: err}
	}
	return fileResult{name: name, outPath: outPath, items: inserted, dupes: dupes}
}
