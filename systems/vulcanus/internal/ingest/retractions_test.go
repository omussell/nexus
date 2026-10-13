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
	if _, err := db.ExecContext(ctx, `CREATE TABLE nauvis (record JSON, doi TEXT, funder_names JSON, affiliation_names JSON)`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO nauvis VALUES ('{"DOI":"10.1/paper","type":"journal-article"}', '10.1/paper', '[]', '[]')`); err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE retractionwatch (record JSON, original_paper_doi TEXT, retraction_doi TEXT)`); err != nil {
		t.Fatalf("create retractionwatch: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO retractionwatch VALUES ('{"OriginalPaperDOI":"10.1/paper","RetractionDOI":"10.1/retract"}', '10.1/paper', '10.1/retract')`); err != nil {
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

	var retractionDOI string
	if err := db2.QueryRowContext(ctx, `SELECT retraction_notice_doi FROM retractions`).Scan(&retractionDOI); err != nil {
		t.Fatalf("query retractions view: %v", err)
	}
	if retractionDOI != "10.1/retract" {
		t.Fatalf("retraction_notice_doi = %q, want 10.1/retract", retractionDOI)
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

	var count int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.views WHERE table_name = 'retractions' AND table_schema = 'main'`).Scan(&count); err != nil {
		t.Fatalf("lookup view retractions: %v", err)
	}
	if count != 0 {
		t.Fatalf("view retractions must not exist when a source table is missing")
	}
}
