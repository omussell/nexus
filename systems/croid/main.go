// Command croid runs the CROID service: it issues Crossref Research Object
// IDs (POST /croid) and serves them back (GET /croid/{croid}), storing them
// in a SQLite database. It also supports generating CROIDs from the CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexus/croid/internal/server"
	"flag"
)

// runGenerate handles the --generate mode.
//
// It defers entirely to the HTTP service's own logic: it opens a server with
// server.New (same SQLite setup + schema apply + store wiring the HTTP path
// uses), then calls the exact same MintCroid/Create path that POST /croid
// runs, so the CLI mints, dedupes, and validates identically to the server.
// No DB/store/SQL setup lives here — it all goes through the server package.
func runGenerate(dbPath, amqpURL, exchange string, inputJSON []byte) error {
	var srv *server.Server
	var err error
	if amqpURL != "" {
		srv, err = server.NewWithAMQP(context.Background(), dbPath, amqpURL, exchange, nil)
	} else {
		srv, err = server.New(context.Background(), dbPath, nil)
	}
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}
	defer srv.Close()

	var input struct {
		CroType  string `json:"cro_type"`
		CroValue string `json:"cro_value"`
		System   string `json:"system"`
	}
	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &input); err != nil {
			return fmt.Errorf("parse input JSON: %w", err)
		}
	}

	rec, _, err := srv.MintCroid(context.Background(), server.Identity{
		CroType:  input.CroType,
		CroValue: input.CroValue,
		System:   input.System,
	}, "")
	if err != nil {
		return fmt.Errorf("mint croid: %w", err)
	}

	// CroidResponse reuses the HTTP handler's exact response shape.
	return json.NewEncoder(os.Stdout).Encode(srv.CroidResponse(rec))
}

// run starts either the HTTP server or a one-shot generate mode.
func run(ctx context.Context, addr, dbPath, amqpURL, exchange string, generate bool, inputJSON []byte) error {
	if generate {
		return runGenerate(dbPath, amqpURL, exchange, inputJSON)
	}

	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	var srv *server.Server
	var err error
	if amqpURL != "" {
		srv, err = server.NewWithAMQP(ctx, dbPath, amqpURL, exchange, log.Printf)
	} else {
		srv, err = server.New(ctx, dbPath, log.Printf)
	}
	if err != nil {
		return err
	}
	defer srv.Close()

	log.Printf("croid: listening on %s", addr)
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
		addr     string
		dbPath   string
		amqpURL  string
		exchange string
		generate bool
		input    string
	)

	flag.StringVar(&addr, "addr", ":8080", "listen address (host:port)")
	flag.StringVar(&dbPath, "db", "croid.sqlite3", "path to the SQLite database file")
	flag.StringVar(&amqpURL, "amqp", "", "RabbitMQ connection URL (e.g. amqp://localhost:5672)")
	flag.StringVar(&exchange, "amqp-exchange", "croid", "RabbitMQ exchange name")
	flag.BoolVar(&generate, "generate", false, "generate a new CROID and output as JSON")
	flag.StringVar(&input, "input", "", "JSON input: {\"cro_type\":\"...\",\"cro_value\":\"...\",\"system\":\"...\"}")
	flag.Parse()

	inputJSON := []byte(input)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, addr, dbPath, amqpURL, exchange, generate, inputJSON); err != nil {
		fmt.Fprintf(os.Stderr, "croid: %v\n", err)
		os.Exit(1)
	}
}
