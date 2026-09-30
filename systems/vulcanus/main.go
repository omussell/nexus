// Command vulcanus ingests CROID events from RabbitMQ into a DuckDB database,
// into separate tables (Nauvis DOI records into a `nauvis` table, each Fulgora
// dataset into its own table). After consuming messages, it can run
// retractionwatch matching (rebuilding the retraction views) and/or ROR
// matching against the nauvis records.
//
// Vulcanus runs as a batch worker: it connects to RabbitMQ, consumes messages
// into DuckDB, and exits on SIGINT/SIGTERM. It can be deployed as a container
// that processes messages until stopped. At least one matching flag
// (-match-retractions or -match-ror) must be given; otherwise vulcanus prints
// usage and exits without doing anything.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nexus/vulcanus/internal/ingest"
	"github.com/nexus/vulcanus/internal/rabbitmq"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	rabbitMQURL := flag.String("rabbitmq", "amqp://localhost:5672", "RabbitMQ AMQP URL")
	exchange := flag.String("exchange", "croid", "RabbitMQ exchange to consume from")
	queue := flag.String("queue", "vulcanus", "RabbitMQ queue name to declare/bind")
	routingKey := flag.String("routing-key", "", "RabbitMQ routing key (empty = all)")
	outDB := flag.String("duckdb", "vulcanus.duckdb", "path to the DuckDB database to write")
	matchROR := flag.Bool("match-ror", false, "run ROR matching against nauvis records after consuming messages")
	matchRetractions := flag.Bool("match-retractions", false, "run retractionwatch matching (rebuild the retraction views) after consuming messages")
	flag.Parse()

	if !*matchROR && !*matchRetractions {
		fmt.Fprintln(os.Stderr, "vulcanus: at least one matching flag is required (-match-ror and/or -match-retractions)")
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()

	// Open DuckDB once and reuse for all ingests.
	db, err := ingest.OpenDB(*outDB)
	if err != nil {
		log.Fatalf("vulcanus: open DuckDB: %v", err)
	}
	defer db.Close()

	// Create the ingest writer that knows how to insert records into DuckDB.
	writer := &ingestWriter{db: db}

	// Set up signal handling for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create the RabbitMQ consumer.
	handler := func(rec rabbitmq.Record) error {
		return writer.Ingest(ctx, rec)
	}

	log.Println("vulcanus: starting consumer")
	consumer, err := rabbitmq.New(*rabbitMQURL, *exchange, *queue, *routingKey, handler, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		log.Fatalf("vulcanus: rabbitmq: %v", err)
	}

	// Wait for shutdown signal or consumer exit.
	log.Println("vulcanus: waiting for messages (send SIGINT/SIGTERM to stop)")
	select {
	case <-sigCh:
		log.Println("vulcanus: shutting down...")
	case <-consumer.Done():
		log.Println("vulcanus: consumer loop exited")
	}

	// Gracefully close with timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Close in a goroutine so we can wait with timeout.
	closeDone := make(chan struct{})
	go func() {
		consumer.Close()
		close(closeDone)
	}()

	select {
	case <-shutdownCtx.Done():
		log.Println("vulcanus: shutdown timed out, forcing exit")
	case <-closeDone:
		// Clean shutdown
	}

	// Run the requested matching.
	if *matchRetractions {
		if err := ingest.RunRetractionMatches(ctx, *outDB); err != nil {
			log.Fatalf("vulcanus: retraction matching: %v", err)
		}
		log.Println("vulcanus: retraction matching complete")
	}

	if *matchROR {
		if err := ingest.RunRORMatches(ctx, *outDB); err != nil {
			log.Fatalf("vulcanus: ROR matching: %v", err)
		}
		log.Println("vulcanus: ROR matching complete")
	}

	log.Printf("vulcanus: done, DuckDB=%s", *outDB)
}

// ingestWriter handles ingesting CROID records into DuckDB tables.
type ingestWriter struct {
	db   *sql.DB
	stmt map[string]*sql.Stmt // table name -> INSERT statement
}

// Ingest inserts a record into the appropriate DuckDB table based on the
// system field (nauvis or fulgora).
func (w *ingestWriter) Ingest(ctx context.Context, rec rabbitmq.Record) error {
	table := w.tableFor(rec)
	if table == "" {
		return fmt.Errorf("unknown system %q, skipping record (croid=%s)", rec.System, rec.Croid)
	}

	stmt, err := w.ensureStmt(table)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}

	_, err = stmt.ExecContext(ctx, rec.Record)
	if err != nil {
		return fmt.Errorf("insert into %s: %w", table, err)
	}

	return nil
}

// tableFor returns the DuckDB table name for a given system and record.
func (w *ingestWriter) tableFor(rec rabbitmq.Record) string {
	switch rec.System {
	case "nauvis":
		return "nauvis"
	case "fulgora":
		// For fulgora, cro_value is "source:version". Extract the source name.
		return parseFulgoraSource(rec.CroValue)
	default:
		return ""
	}
}

// parseFulgoraSource extracts the source name from "source:version" format.
func parseFulgoraSource(croValue string) string {
	for i := 0; i < len(croValue); i++ {
		if croValue[i] == ':' {
			return croValue[:i]
		}
	}
	return "fulgora"
}

func (w *ingestWriter) ensureStmt(table string) (*sql.Stmt, error) {
	if w.stmt == nil {
		w.stmt = make(map[string]*sql.Stmt)
	}
	if s, ok := w.stmt[table]; ok {
		return s, nil
	}

	// Ensure the table exists BEFORE preparing the INSERT statement.
	if err := ensureTable(context.Background(), w.db, table); err != nil {
		return nil, fmt.Errorf("create table %s: %w", table, err)
	}

	query := fmt.Sprintf("INSERT INTO %s (record) VALUES (?)", table)
	stmt, err := w.db.PrepareContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("prepare %s: %w", table, err)
	}
	w.stmt[table] = stmt

	return stmt, nil
}

// ensureTable creates the table if it doesn't exist.
func ensureTable(ctx context.Context, db *sql.DB, name string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (record JSON)
	`, name)
	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("create table %s: %w", name, err)
	}

	return nil
}
