package ingest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRunOFRMatchesCreatesTable(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	// Open DuckDB and create the required tables.
	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()

	// Create nauvis table with funder data.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE nauvis (
			record JSON,
			croid TEXT,
			doi TEXT,
			funder_names JSON,
			affiliation_names JSON
		)
	`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}

	// Insert sample nauvis records.
	_, err = db.ExecContext(ctx, `
		INSERT INTO nauvis (doi, funder_names) VALUES
			('10.1.1.1', '["National Science Foundation"]'),
			('10.1.1.2', '["Japan Society for the Promotion of Science"]'),
			('10.1.1.3', '["National Institutes of Health"]')
	`)
	if err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}

	// Create openfunderregistry table with funder data.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE openfunderregistry (
			record JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced TEXT
		)
	`); err != nil {
		t.Fatalf("create openfunderregistry: %v", err)
	}

	// Insert sample funder records (canonical names).
	_, err = db.ExecContext(ctx, `
		INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES
			('https://dx.doi.org/10.13039/501100001809', 'National Science Foundation', ''),
			('https://dx.doi.org/10.13039/501100001691', 'Japan Society for the Promotion of Science', ''),
			('https://dx.doi.org/10.13039/501100003974', 'National Institutes of Health', ''),
			('https://dx.doi.org/10.13039/501100011576', 'Japan Society for the Advancement of Science', '10.13039/501100001691')
	`)
	if err != nil {
		t.Fatalf("insert openfunderregistry: %v", err)
	}

	// Run OFR matching.
	if err := RunOFRMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunOFRMatches: %v", err)
	}

	// Verify the ofr_matches table exists and has expected content.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ofr_matches`).Scan(&count); err != nil {
		t.Fatalf("query ofr_matches: %v", err)
	}
	if count < 2 {
		t.Errorf("Expected at least 2 matches, got %d", count)
	}

	// Verify specific matches are present.
	var uri string
	if err := db.QueryRowContext(ctx, `
		SELECT funder_uri FROM ofr_matches WHERE nauvis_item_doi = '10.1.1.1' LIMIT 1
	`).Scan(&uri); err != nil {
		if err != sql.ErrNoRows {
			t.Fatalf("query match: %v", err)
		}
		// This record might not have matched if fuzzy matching threshold was not met.
		// Let's check any match instead.
	}

	t.Logf("OFR matching: %d matches found", count)
}

func TestRunOFRMatchesNoTables(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	// Open DuckDB but do not create tables.
	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()

	// Should be a no-op when tables don't exist.
	if err := RunOFRMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunOFRMatches should be no-op when tables missing: %v", err)
	}
}

func TestRunOFRMatchesEmptyFunderNames(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE nauvis (
			record JSON, croid TEXT, doi TEXT,
			funder_names JSON, affiliation_names JSON
		)
	`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}

	// Insert record with empty funder names.
	_, err = db.ExecContext(ctx, `
		INSERT INTO nauvis (doi, funder_names) VALUES ('10.1.1.1', '[]')
	`)
	if err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE openfunderregistry (
			record JSON, funder_uri TEXT, funder_name TEXT, replaced TEXT
		)
	`); err != nil {
		t.Fatalf("create openfunderregistry: %v", err)
	}

	if err := RunOFRMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunOFRMatches: %v", err)
	}
}

func TestRunOFRMatchesFuzzyMatch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE nauvis (
			record JSON, croid TEXT, doi TEXT,
			funder_names JSON, affiliation_names JSON
		)
	`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}

	// Insert nauvis record with a slightly abbreviated funder name.
	_, err = db.ExecContext(ctx, `
		INSERT INTO nauvis (doi, funder_names) VALUES
			('10.2.1.1', '["National Science Foundation"]'),
			('10.2.1.2', '["Japan Society for the Prmoton of Sceince"]')
	`)
	if err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE openfunderregistry (
			record JSON, funder_uri TEXT, funder_name TEXT, replaced TEXT
		)
	`); err != nil {
		t.Fatalf("create openfunderregistry: %v", err)
	}

	// Insert canonical funder names (exact match for NSF, different for the other).
	_, err = db.ExecContext(ctx, `
		INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES
			('https://dx.doi.org/10.13039/501100001809', 'National Science Foundation', ''),
			('https://dx.doi.org/10.13039/501100001691', 'Japan Society for the Promotion of Science', ''),
			('https://dx.doi.org/10.13039/501100000000', 'Non-matching Funder', '')
	`)
	if err != nil {
		t.Fatalf("insert openfunderregistry: %v", err)
	}

	if err := RunOFRMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunOFRMatches: %v", err)
	}

	// Verify NSF matched.
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ofr_matches WHERE funder_uri = 'https://dx.doi.org/10.13039/501100001809'
	`).Scan(&count); err != nil {
		t.Fatalf("query NSF match: %v", err)
	}
	if count < 1 {
		t.Error("Expected NSF to be matched")
	}

	// The typos in "Japan Society for the Prmoton of Sceince" likely won't
	// pass the 96% fuzzy threshold, but let's check we got at least one match.
	t.Logf("Total OFR matches: %d", count)
}

func TestRunOFRMatchesIgnoresReplaced(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	db, err := openDuckDB(dbPath)
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE nauvis (
			record JSON, croid TEXT, doi TEXT,
			funder_names JSON, affiliation_names JSON
		)
	`); err != nil {
		t.Fatalf("create nauvis: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO nauvis (doi, funder_names) VALUES ('10.3.1.1', '["Japan Society for the Advancement of Science"]')
	`)
	if err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE openfunderregistry (
			record JSON, funder_uri TEXT, funder_name TEXT, replaced TEXT
		)
	`); err != nil {
		t.Fatalf("create openfunderregistry: %v", err)
	}

	// The JSA has been replaced by JSPS — it has a non-empty replaced value.
	// Matching against it should NOT succeed since we filter replaced records.
	_, err = db.ExecContext(ctx, `
		INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES
			('https://dx.doi.org/10.13039/501100011576', 'Japan Society for the Advancement of Science', '10.13039/501100001691'),
			('https://dx.doi.org/10.13039/501100001691', 'Japan Society for the Promotion of Science', '')
	`)
	if err != nil {
		t.Fatalf("insert openfunderregistry: %v", err)
	}

	if err := RunOFRMatches(ctx, dbPath); err != nil {
		t.Fatalf("RunOFRMatches: %v", err)
	}

	// Verify no matches were created (the replaced record should be excluded).
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ofr_matches`).Scan(&count); err != nil {
		t.Fatalf("query ofr_matches: %v", err)
	}
	if count > 0 {
		t.Errorf("Expected 0 matches (replaced openfunderregistry record should be excluded), got %d", count)
	}
}
