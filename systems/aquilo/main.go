// Command aquello copies nauvis data from a vulcanus DuckDB database into a
// local SQLite store and serves it via HTTP.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/nexus/aquilo/internal/server"
	"github.com/nexus/aquilo/internal/store"
	"github.com/nexus/aquilo/internal/sync"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "sync":
		runSync(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "aquilo: unknown command %q\n", command)
		printUsage()
		os.Exit(1)
	}
}

func runSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	duckdbPath := fs.String("duckdb", "vulcanus.duckdb", "path to vulcanus DuckDB database")
	dbPath := fs.String("db", "aquilo.db", "path to local SQLite database")
	batchSize := fs.Int("batch", 1000, "records per batch insert")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("aquilo sync: parse flags: %v", err)
	}

	ctx := context.Background()

	result, err := sync.Run(ctx, sync.Config{
		DuckDBPath: *duckdbPath,
		DBPath:     *dbPath,
		BatchSize:  *batchSize,
	})
	if err != nil {
		log.Fatalf("aquilo sync: %v", err)
	}

	fmt.Printf("aquilo: sync complete: %d/%d records synced, %d errors\n",
		result.Synced, result.Total, result.Errors)
	if result.LastCroid != "" {
		fmt.Printf("aquilo: last CROID: %s\n", result.LastCroid)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "aquilo.db", "path to local SQLite database")
	listen := fs.String("listen", "localhost:8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("aquilo serve: parse flags: %v", err)
	}

	ctx := context.Background()

	// Resolve relative path from CWD.
	if !filepath.IsAbs(*dbPath) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*dbPath = filepath.Join(wd, *dbPath)
	}

	s, conn, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Fatalf("aquilo: open db: %v", err)
	}
	defer conn.Close()

	srv := server.New(*listen, s)
	if err := srv.WaitSignal(ctx); err != nil {
		log.Fatalf("aquilo: %v", err)
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dbPath := fs.String("db", "aquilo.db", "path to local SQLite database")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("aquilo status: parse flags: %v", err)
	}

	ctx := context.Background()

	// Resolve relative path from CWD.
	if !filepath.IsAbs(*dbPath) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*dbPath = filepath.Join(wd, *dbPath)
	}

	s, conn, err := store.Open(ctx, *dbPath)
	if err != nil {
		log.Fatalf("aquilo: open db: %v", err)
	}
	defer conn.Close()

	count, err := s.CountItems(ctx)
	if err != nil {
		log.Fatalf("aquilo: count items: %v", err)
	}

	fmt.Printf("aquilo: %d items stored in %s\n", count, *dbPath)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `
aquilo - copy nauvis data from vulcanus and serve it

Usage:
  aquilo <command> [arguments]

Commands:
  sync     Copy nauvis records from DuckDB to SQLite
  serve    Start HTTP server to serve stored items
  status   Show sync status (item count)

Sync flags:
  -duckdb path   Path to vulcanus.duckdb (default "vulcanus.duckdb")
  -db path       Path to SQLite DB (default "aquilo.db")
  -batch n       Records per batch (default 1000)

Serve flags:
  -db path       Path to SQLite DB (default "aquilo.db")
  -listen addr   HTTP listen address (default "localhost:8080")

Status flags:
  -db path       Path to SQLite DB (default "aquilo.db")
`)
}
