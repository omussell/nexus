// Command fulgora manages third-party dataset collection and discovery.
//
// Subcommands:
//
//	fulgora fetch   [source ...]   check for & download new dataset versions
//	fulgora status  [source ...]   print the latest collected version per source
//	fulgora serve                 start the HTTP discovery service
//
// If no source is named, all registered sources are used.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (pure Go)

	"github.com/nexus/fulgora/internal/collect"
	"github.com/nexus/fulgora/internal/db"
	"github.com/nexus/fulgora/internal/migrate"
	"github.com/nexus/fulgora/internal/server"
	"github.com/nexus/fulgora/internal/sources"
	"github.com/nexus/fulgora/internal/store"
)

const defaultAddr = ":8090"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Printf("fulgora: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage(nil)
	}
	switch args[0] {
	case "fetch":
		return runFetch(ctx, args[1:])
	case "status":
		return runStatus(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	case "help", "--help", "-h":
		return usage(nil)
	default:
		return usage(fmt.Errorf("unknown command %q", args[0]))
	}
}

func usage(err error) error {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
	}
	fmt.Fprint(os.Stderr, `usage: fulgora <command> [source ...]

commands:
  fetch    check for & download new dataset versions
  status   print the latest collected version per source
  serve    start the HTTP discovery service

If no source is named, all registered sources are used.
available sources: `+strings.Join(sources.All(), ", "))
	return err
}

// openStore opens the SQLite database at dbPath, applies the schema, and
// returns a ready store plus a close function.
func openStore(ctx context.Context, dbPath string) (*store.Store, func() error, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, nil, errors.New("empty database path")
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
	return store.New(db.New(conn)), closeFn, nil
}

// runFetch implements `fulgora fetch [source ...]`.
func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("FULGORA_DB", "fulgora.sqlite3"), "path to the SQLite database file")
	root := fs.String("root", envOr("FULGORA_ROOT", "."), "root directory under which per-source dirs live")
	if err := fs.Parse(args); err != nil {
		return err
	}
	want := fs.Args()

	names, err := resolveSources(want)
	if err != nil {
		return err
	}

	s, closeFn, err := openStore(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer closeFn()

	var anyErr error
	for _, name := range names {
		src, err := sources.Get(name)
		if err != nil {
			anyErr = err
			continue
		}
		res, err := collect.Collect(ctx, src, s, *root)
		if err != nil {
			log.Printf("[%s] %v", name, err)
			anyErr = err
			continue
		}
		if res.Skipped {
			log.Printf("[%s] %s up to date", name, res.Version)
		} else {
			log.Printf("[%s] collected %s -> %s", name, res.Version, res.Output)
		}
	}
	return anyErr
}

// runStatus implements `fulgora status [source ...]`.
func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("FULGORA_DB", "fulgora.sqlite3"), "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	want := fs.Args()

	names, err := resolveSources(want)
	if err != nil {
		return err
	}

	s, closeFn, err := openStore(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer closeFn()

	var anyErr error
	for _, name := range names {
		info, err := s.Latest(ctx, name)
		switch {
		case err == nil:
			fmt.Printf("%-20s %s   (collected %s)\n", name, info.Version, info.CollectedAt)
		case errors.Is(err, sql.ErrNoRows):
			fmt.Printf("%-20s (none collected yet)\n", name)
		default:
			fmt.Printf("%-20s <error: %v>\n", name, err)
			anyErr = err
		}
	}
	return anyErr
}

// runServe implements `fulgora serve`.
func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", envOr("FULGORA_ADDR", defaultAddr), "listen address (host:port)")
	dbPath := fs.String("db", envOr("FULGORA_DB", "fulgora.sqlite3"), "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	log.SetOutput(os.Stderr)

	srv, err := server.New(ctx, *dbPath, log.Printf)
	if err != nil {
		return err
	}
	defer srv.Close()

	log.Printf("fulgora: listening on %s (db=%s)", *addr, *dbPath)
	hs := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Printf("fulgora: shutting down")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hs.Shutdown(shutdownCtx)
}

// resolveSources turns a CLI argument list into source names. An empty list
// means "all registered sources".
func resolveSources(names []string) ([]string, error) {
	if len(names) == 0 {
		return sources.All(), nil
	}
	for _, n := range names {
		if _, err := sources.Get(n); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
