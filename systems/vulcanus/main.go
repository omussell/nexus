// Command vulcanus ingests CROID events from RabbitMQ into a DuckDB database,
// into separate tables (Nauvis DOI records into a `nauvis` table, each Fulgora
// dataset into its own table). After receiving all messages (or on demand via
// -match-ror), it can run ROR matching against the nauvis records.
//
// Vulcanus runs as a batch worker: it connects to RabbitMQ, consumes messages
// into DuckDB, and exits on SIGINT/SIGTERM. It can be deployed as a container
// that processes messages until stopped.
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
	flag.Parse()

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

	// Optionally run ROR matching.
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

	// Also ensure the retraction views exist.
	if err := buildRetractionsView(ctx, db); err != nil {
		return fmt.Errorf("build retraction views: %w", err)
	}

	return nil
}

// buildRetractionsView creates the retraction-related views if both tables exist.
func buildRetractionsView(ctx context.Context, db *sql.DB) error {
	// Check if both nauvis and retractionwatch tables exist.
	var nauvisExists, rwExists bool
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0 FROM information_schema.tables
		WHERE table_schema = 'main' AND table_name = 'nauvis'
	`).Scan(&nauvisExists)
	if err != nil {
		return fmt.Errorf("check nauvis table: %w", err)
	}
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0 FROM information_schema.tables
		WHERE table_schema = 'main' AND table_name = 'retractionwatch'
	`).Scan(&rwExists)
	if err != nil {
		return fmt.Errorf("check retractionwatch table: %w", err)
	}

	if !nauvisExists || !rwExists {
		return nil
	}

	// Create retractions view.
	_, err = db.ExecContext(ctx, `
		CREATE OR REPLACE VIEW retractions AS
		SELECT
			n.record AS nauvis_record,
			r.record AS retractionwatch_record,
			json_extract_string(n.record, '$.DOI') AS matched_doi
		FROM nauvis AS n
		JOIN retractionwatch AS r
		  ON json_extract_string(n.record, '$.DOI') =
		     json_extract_string(r.record, '$.OriginalPaperDOI')
	`)
	if err != nil {
		return fmt.Errorf("create retractions view: %w", err)
	}

	// Create retraction_provenance view.
	_, err = db.ExecContext(ctx, `
		CREATE OR REPLACE VIEW retraction_provenance AS
		SELECT
			n.record AS nauvis_record, r.record AS retractionwatch_record,
			json_extract_string(n.record, '$.DOI') AS paper_doi,
			json_extract_string(r.record, '$.RetractionDOI') AS retraction_doi,
			json_extract_string(n.record, '$.DOI') || ' is-retracted-by ' || json_extract_string(r.record, '$.RetractionDOI') AS provenance
		FROM nauvis AS n
		JOIN retractionwatch AS r ON json_extract_string(n.record, '$.DOI') = json_extract_string(r.record, '$.OriginalPaperDOI')
		UNION ALL
		SELECT
			r.record AS nauvis_record, n.record AS retractionwatch_record,
			json_extract_string(r.record, '$.RetractionDOI') AS paper_doi,
			json_extract_string(n.record, '$.DOI') AS retraction_doi,
			json_extract_string(r.record, '$.RetractionDOI') || ' retracts ' || json_extract_string(n.record, '$.DOI') AS provenance
		FROM retractionwatch AS r
		JOIN nauvis AS n ON json_extract_string(n.record, '$.DOI') = json_extract_string(r.record, '$.OriginalPaperDOI')
	`)
	if err != nil {
		return fmt.Errorf("create retraction_provenance view: %w", err)
	}

	return nil
}
