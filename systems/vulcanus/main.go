// Command vulcanus ingests the NDJSON files the nauvis and fulgora systems
// wrote out into a DuckDB database, into separate tables (Nauvis into a single
// `nauvis` table, each Fulgora source into its own table). It can also start an
// HTTP matching server against the resulting DuckDB database.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"github.com/nexus/vulcanus/internal/fulgora"
	"github.com/nexus/vulcanus/internal/ingest"
	"github.com/nexus/vulcanus/internal/nauvis"
	"github.com/nexus/vulcanus/internal/server"
)

func main() {
	only := flag.String("only", "all", "which provider to ingest: nauvis, fulgora, or all")
	nauvisDB :=
		flag.String("nauvis-db", "nauvis.sqlite3", "path to the nauvis SQLite database")
	nauvisPath := flag.String("nauvis-dir", "", "path to the nauvis system directory (data root)")
	fulgoraDB :=
		flag.String("fulgora-db", "fulgora.sqlite3", "path to the fulgora SQLite database")
	fulgoraPath :=
		flag.String("fulgora-dir", "", "path to the fulgora data root (output dir)")
	outDB :=
		flag.String("duckdb", "vulcanus.duckdb", "path to the DuckDB database to write")
	flag.Parse()

	if *only != "nauvis" && *only != "fulgora" && *only != "all" {
		log.Fatalf("vulcanus: -only must be nauvis, fulgora, or all (got %q)", *only)
	}
	if *only == "nauvis" && *nauvisPath == "" {
		log.Fatal("vulcanus: -nauvis-dir is required to ingest nauvis data")
	}
	if *only == "fulgora" && *fulgoraPath == "" {
		log.Fatal("vulcanus: -fulgora-dir is required to ingest fulgora data")
	}
	if *only == "all" && (*nauvisPath == "" || *fulgoraPath == "") {
		log.Fatal("vulcanus: both -nauvis-dir and -fulgora-dir are required when -only=all")
	}

	ctx := context.Background()

	if doNauvis(*only) {
		n, err := runNauvis(ctx, *nauvisDB, *nauvisPath, *outDB)
		if err != nil {
			log.Fatalf("vulcanus: nauvis ingest: %v", err)
		}
		fmt.Printf("vulcanus: ingested %d nauvis records into %s\n", n, filepath.Clean(*outDB))
	}

	if doFulgora(*only) {
		n, err := runFulgora(ctx, *fulgoraDB, *fulgoraPath, *outDB)
		if err != nil {
			log.Fatalf("vulcanus: fulgora ingest: %v", err)
		}
		fmt.Printf("vulcanus: ingested %d fulgora records into %s\n", n, filepath.Clean(*outDB))
	}

	// Try to start the matching server if the DuckDB exists and a server port is provided.
	serverPort := flag.Int("server-port", 0, "start HTTP matching server on this port (0 to disable)")
	if *serverPort > 0 {
		cfg := server.Config{
			Host:   "0.0.0.0",
			Port:   *serverPort,
			DBPath: *outDB,
		}

		srv, err := server.New(cfg)
		if err != nil {
			log.Fatalf("vulcanus: create server: %v", err)
		}
		defer srv.Close()
		if err := srv.Start(); err != nil {
			log.Fatalf("vulcanus: server: %v", err)
		}
	}
}

// doNauvis reports whether the selected provider includes Nauvis.
func doNauvis(only string) bool {
	return only == "nauvis" || only == "all"
}

// doFulgora reports whether the selected provider includes Fulgora.
func doFulgora(only string) bool {
	return only == "fulgora" || only == "all"
}

// runNauvis opens the Nauvis store and ingests it into outDB, returning the
// number of records loaded.
func runNauvis(ctx context.Context, dbPath, outDir, outDB string) (int, error) {
	vis, err := nauvis.Open(dbPath)
	if err != nil {
		return 0, fmt.Errorf("open nauvis store: %w", err)
	}
	defer vis.Close()
	return ingest.Run(ctx, vis, outDir, outDB)
}

// runFulgora opens the Fulgora store and ingests it into outDB, returning the
// number of records loaded.
func runFulgora(ctx context.Context, dbPath, outDir, outDB string) (int, error) {
	store, err := fulgora.Open(dbPath, outDir)
	if err != nil {
		return 0, fmt.Errorf("open fulgora store: %w", err)
	}
	defer store.Close()
	return ingest.Run(ctx, store, outDir, outDB)
}
