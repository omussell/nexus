// Command croid runs the CROID service: it issues Crossref Research Object
// IDs (POST /croid) and serves them back (GET /croid/{croid}), storing them
// in a SQLite database.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexus/croid/internal/server"
)

const (
	defaultListen = ":8080"
	defaultDB     = "croid.sqlite3"
)

func run(ctx context.Context, addr, dbPath string) error {
	log.SetFlags(0)

	srv, err := server.New(ctx, dbPath, log.Printf)
	if err != nil {
		return err
	}
	defer srv.Close()

	log.Printf("croid: listening on %s (db=%s)", addr, dbPath)

	hs := &http.Server{
		Addr:              addr,
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
		log.Printf("croid: shutting down")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hs.Shutdown(shutdownCtx)
}

func main() {
	var (
		addr   string
		dbPath string
	)

	flag.StringVar(&addr, "addr", envOr("CROID_ADDR", defaultListen), "listen address (host:port)")
	flag.StringVar(&dbPath, "db", envOr("CROID_DB", defaultDB), "path to the SQLite database file")
	flag.Parse()

	log.SetOutput(os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, addr, dbPath); err != nil {
		log.Printf("croid: %v", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
