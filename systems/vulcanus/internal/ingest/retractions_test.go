package ingest

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRunRetractionMatches_BuildsViews(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE nauvis (record JSON)`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO nauvis VALUES ('{"DOI":"10.1/paper"}')`); err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE retractionwatch (record JSON)`); err != nil {
		t.Fatalf("create retractionwatch: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO retractionwatch VALUES ('{"OriginalPaperDOI":"10.1/paper","RetractionDOI":"10.1/retract"}')`); err != nil {
		t.Fatalf("insert retractionwatch: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if err := RunRetractionMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunRetractionMatches: %v", err)
	}

	db2, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("reopen DuckDB: %v", err)
	}
	defer db2.Close()

	var matched string
	if err := db2.QueryRowContext(ctx, `SELECT matched_doi FROM retractions`).Scan(&matched); err != nil {
		t.Fatalf("query retractions view: %v", err)
	}
	if matched != "10.1/paper" {
		t.Fatalf("matched_doi = %q, want 10.1/paper", matched)
	}

	var provenance string
	if err := db2.QueryRowContext(ctx, `SELECT provenance FROM retraction_provenance WHERE paper_doi = '10.1/paper'`).Scan(&provenance); err != nil {
		t.Fatalf("query retraction_provenance view: %v", err)
	}
	if provenance != "10.1/paper is-retracted-by 10.1/retract" {
		t.Fatalf("provenance = %q, want %q", provenance, "10.1/paper is-retracted-by 10.1/retract")
	}
}

func TestRunRetractionMatches_NoOpWhenTablesMissing(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE nauvis (record JSON)`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if err := RunRetractionMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunRetractionMatches must be a no-op without both tables: %v", err)
	}

	db2, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("reopen DuckDB: %v", err)
	}
	defer db2.Close()

	for _, view := range []string{"retractions", "retraction_provenance"} {
		var count int
		if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.views WHERE table_name = ? AND table_schema = 'main'`, view).Scan(&count); err != nil {
			t.Fatalf("lookup view %s: %v", view, err)
		}
		if count != 0 {
			t.Fatalf("view %s must not exist when a source table is missing", view)
		}
	}
}
