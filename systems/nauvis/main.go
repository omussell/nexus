// Command nauvis extracts the Crossref snapshot dataset and records its items.
//
// It reads the gzip-compressed item files (*.json.gz) from an input directory
// and, for each one, decompresses the payload, validates that it is a single
// well-formed JSON value, writes it out as a plain .json file with
// byte-for-byte identical content, and records the DOIs of its items in a
// SQLite database. Thousands of input files are processed in parallel across a
// pool of workers.
//
// Two modes are supported:
//
//	process  (default) - extract + record (see -in/-out/-db/-jobs)
//	-query   "DOI"     - look up a recorded DOI and print its file
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/nexus/nauvis/internal/ingest"
	"github.com/nexus/nauvis/internal/store"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	query := flag.String("query", "", "look up a recorded DOI and print its file (implies -db)")
	inDir := flag.String("in", "data", "directory containing *.json.gz input files")
	outDir := flag.String("out", "out", "directory to write decompressed *.json files")
	dbPath := flag.String("db", "nauvis.sqlite3", "path to the SQLite database file")
	jobs := flag.Int("jobs", 0, "number of parallel workers (default GOMAXPROCS)")
	flag.Parse()

	ctx := context.Background()

	if strings.TrimSpace(*query) != "" {
		st, conn, err := store.Open(ctx, *dbPath)
		if err != nil {
			log.Printf("nauvis: %v", err)
			os.Exit(1)
		}
		defer conn.Close()
		item, err := st.GetByDOI(ctx, *query)
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Printf("%s: no such DOI\n", *query)
			os.Exit(1)
		}
		if err != nil {
			log.Printf("nauvis: %v", err)
			os.Exit(1)
		}
		fmt.Printf("%s -> %s\n", item.Doi, item.File)
		return
	}

	// Default mode: extract + record.
	st, conn, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Printf("nauvis: %v", err)
		os.Exit(1)
	}
	defer conn.Close()

	lg := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ok, failed, err := ingest.Run(ctx, *inDir, *outDir, st, *jobs, lg)
	if failed > 0 {
		log.Printf("nauvis: %d ok, %d failed: %v", ok, failed, err)
		os.Exit(1)
	}
	log.Printf("nauvis: %d ok, 0 failed, db=%s", ok, *dbPath)
}
