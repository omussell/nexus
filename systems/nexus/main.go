// Command nexus manages relationship records and provenance.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/nexus/nexus/internal/db"
	"github.com/nexus/nexus/internal/fetcher"
	"github.com/nexus/nexus/internal/migrate"
	"github.com/nexus/nexus/internal/store"
)

const defaultDB = "nexus.db"

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("nexus: %v", err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	ctx := context.Background()

	switch args[0] {
	case "init":
		return runInit(ctx, args[1:])
	case "fetch":
		return runFetch(ctx, args[1:])
	case "help", "--help", "-h":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() error {
	fmt.Fprint(os.Stderr, `usage: nexus <command> [options]

commands:
  init      initialize the database
  fetch     load relationships from vulcanus (nauvis, ROR, retractionwatch, openfunderregistry) and fulgora
`)
	return nil
}

func openDB(ctx context.Context, dbPath string) (*store.Store, func() error, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = defaultDB
	}
	dsn := "file:" + dbPath + "?_fk=1&_busy_timeout=5000"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	conn.SetMaxOpenConns(1)
	closeFn := func() error { return conn.Close() }

	if err := conn.PingContext(ctx); err != nil {
		closeFn()
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		closeFn()
		return nil, nil, err
	}
	if err := migrate.Apply(ctx, conn); err != nil {
		closeFn()
		return nil, nil, err
	}
	st, err := store.New(conn, db.New(conn))
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return st, closeFn, nil
}

func runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("NEXUS_DB", defaultDB), "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	_, closeFn, err := openDB(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer closeFn()

	fmt.Println("initialized")
	return nil
}

func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("NEXUS_DB", defaultDB), "path to the Nexus SQLite database")
	vulcanusPath := fs.String("vulcanus", envOr("VULCANUS_DB", "vulcanus.duckdb"), "path to the Vulcanus DuckDB database")
	fulgoraPath := fs.String("fulgora", envOr("FULGORA_DB", "fulgora.db"), "path to the Fulgora SQLite database")
	if err := fs.Parse(args); err != nil {
		return err
	}

	nexusStore, closeFn, err := openDB(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer closeFn()

	lg := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	fmt.Printf("nexus: fetching relationships from %s\n", *vulcanusPath)
	if err := fetcher.Run(ctx, *vulcanusPath, *fulgoraPath, nexusStore, lg); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	fmt.Println("done")
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
