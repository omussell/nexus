// Command vulcanus ingests the NDJSON files Nauvis wrote out into a DuckDB
// database.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"github.com/nexus/vulcanus/internal/ingest"
	"github.com/nexus/vulcanus/internal/nauvis"
)

func main() {
	nauvisDB := flag.String("db", "nauvis.sqlite3", "path to the Nauvis SQLite database")
	nauvisPath := flag.String("visdir", "", "path to the Nauvis system directory")
	outDB := flag.String("duckdb", "vulcanus.duckdb", "path to the DuckDB database to write")
	flag.Parse()

	ctx := context.Background()

	vis, err := nauvis.Open(*nauvisDB)
	if err != nil {
		log.Fatalf("vulcanus: open nauvis store: %v", err)
	}
	defer vis.Close()

	total, err := ingest.Run(ctx, vis, *nauvisPath, *outDB)
	if err != nil {
		log.Fatalf("vulcanus: ingest: %v", err)
	}
	fmt.Printf("vulcanus: ingested %d items into %s\n", total, filepath.Clean(*outDB))
}
