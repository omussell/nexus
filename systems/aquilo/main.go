// Package main is the entry point for aquilo.
//
// Aquilo serves nauvis records from a vulcanus DuckDB database and
// relationship triples from nexus SQLite over HTTP.
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
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
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

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	duckdbPath := fs.String("duckdb", "vulcanus.duckdb", "path to vulcanus DuckDB database")
	nexusDB := fs.String("nexusdb", "nexus.db", "path to nexus SQLite database")
	listen := fs.String("listen", "localhost:8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("aquilo serve: parse flags: %v", err)
	}

	ctx := context.Background()

	// Resolve relative paths from CWD.
	if !filepath.IsAbs(*duckdbPath) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*duckdbPath = filepath.Join(wd, *duckdbPath)
	}
	if !filepath.IsAbs(*nexusDB) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*nexusDB = filepath.Join(wd, *nexusDB)
	}

	s, err := store.NewStore(*duckdbPath, *nexusDB)
	if err != nil {
		log.Fatalf("aquilo: %v", err)
	}
	defer s.Close()

	srv := server.New(*listen, s)
	if err := srv.WaitSignal(ctx); err != nil {
		log.Fatalf("aquilo: %v", err)
	}
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	duckdbPath := fs.String("duckdb", "vulcanus.duckdb", "path to vulcanus DuckDB database")
	nexusDB := fs.String("nexusdb", "nexus.db", "path to nexus SQLite database")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("aquilo status: parse flags: %v", err)
	}

	ctx := context.Background()

	// Resolve relative paths from CWD.
	if !filepath.IsAbs(*duckdbPath) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*duckdbPath = filepath.Join(wd, *duckdbPath)
	}
	if !filepath.IsAbs(*nexusDB) {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("aquilo: getcwd: %v", err)
		}
		*nexusDB = filepath.Join(wd, *nexusDB)
	}

	s, err := store.NewStore(*duckdbPath, *nexusDB)
	if err != nil {
		log.Fatalf("aquilo: %v", err)
	}
	defer s.Close()

	nauvisCount, err := s.NauvisCount(ctx)
	if err != nil {
		log.Fatalf("aquilo: count nauvis: %v", err)
	}

	relCount, err := s.RelationshipCount(ctx)
	if err != nil {
		log.Fatalf("aquilo: count relationships: %v", err)
	}

	fmt.Printf("aquilo: %d nauvis records, %d relationships in %s\n",
		nauvisCount, relCount, *duckdbPath)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `
aquilo - serve nauvis data and relationships from vulcanus/nexus

Usage:
  aquilo <command> [arguments]

Commands:
  serve    Start HTTP server (GET /croid/{croid}, GET /relationships/{croid})
  status   Show record counts

Serve flags:
  -duckdb path   Path to vulcanus DuckDB (default "vulcanus.duckdb")
  -nexusdb path  Path to nexus SQLite (default "nexus.db")
  -listen addr   HTTP listen address (default "localhost:8080")

Status flags:
  -duckdb path   Path to vulcanus DuckDB (default "vulcanus.duckdb")
  -nexusdb path  Path to nexus SQLite (default "nexus.db")
`)
}
