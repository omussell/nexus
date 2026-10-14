package fetcher_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
	_ "modernc.org/sqlite"

	"github.com/nexus/nexus/internal/db"
	"github.com/nexus/nexus/internal/fetcher"
	"github.com/nexus/nexus/internal/migrate"
	"github.com/nexus/nexus/internal/store"
)

func openDuckDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := duckdb.NewConnector(path, nil)
	if err != nil {
		t.Fatalf("connect duckdb: %v", err)
	}
	db := sql.OpenDB(conn)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openSQLiteDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+path+"?_fk=1&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	return conn
}

func openNexusStore(t *testing.T, path string) *store.Store {
	t.Helper()
	conn := openSQLiteDB(t, path)
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s, err := store.New(conn, db.New(conn))
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	return s
}

func setupDuckDB(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE nauvis (
			record JSON,
			croid TEXT,
			doi TEXT
		);
		CREATE TABLE ror_funder_matches (
			paper_doi TEXT,
			name TEXT,
			ror_id TEXT,
			ror_name TEXT,
			confidence DOUBLE
		);
		CREATE TABLE ror_affiliation_matches (
			paper_doi TEXT,
			name TEXT,
			ror_id TEXT,
			ror_name TEXT,
			confidence DOUBLE
		);
		CREATE TABLE retractionwatch (
			record JSON,
			original_paper_doi TEXT,
			retraction_doi TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create duckdb tables: %v", err)
	}

	// Insert nauvis records.
	_, err = db.Exec(`
		INSERT INTO nauvis (croid, doi, record) VALUES
			('nauvis1croidxxxxxxxxxxxxxxxxxxxxxx', '10.1/test', '{"title":"Test 1"}'),
			('nauvis2croidxxxxxxxxxxxxxxxxxxxxxx', '10.2/test', '{"title":"Test 2"}'),
			('nauvis3croidxxxxxxxxxxxxxxxxxxxxxx', '10.3/test', '{"title":"Test 3"}');
	`)
	if err != nil {
		t.Fatalf("insert nauvis: %v", err)
	}

	// Insert ROR funder matches.
	_, err = db.Exec(`
		INSERT INTO ror_funder_matches (paper_doi, name, ror_id, ror_name, confidence) VALUES
			('10.1/test', 'NSF', '01234abcd', 'National Science Foundation', 98.5);
	`)
	if err != nil {
		t.Fatalf("insert ror_funder: %v", err)
	}

	// Insert ROR affiliation matches.
	_, err = db.Exec(`
		INSERT INTO ror_affiliation_matches (paper_doi, name, ror_id, ror_name, confidence) VALUES
			('10.2/test', 'MIT', '05678efgh', 'Massachusetts Institute of Technology', 97.0);
	`)
	if err != nil {
		t.Fatalf("insert ror_affil: %v", err)
	}

	// Insert retraction matches.
	_, err = db.Exec(`
		INSERT INTO retractionwatch (original_paper_doi, retraction_doi, record) VALUES
			('10.3/test', '10.99/retraction', '{"reason":"Error"}');
	`)
	if err != nil {
		t.Fatalf("insert retraction: %v", err)
	}
}

func setupFulgoraDB(t *testing.T, db *sql.DB, rorCroid, retrCroid string) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE datasets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			version TEXT NOT NULL,
			collected_at TEXT NOT NULL,
			initial_input TEXT NOT NULL,
			output TEXT NOT NULL,
			croid TEXT,
			UNIQUE (source, version)
		);
		CREATE TABLE latest (
			source TEXT PRIMARY KEY,
			version TEXT NOT NULL,
			dataset_id INTEGER NOT NULL REFERENCES datasets (id)
		);
	`)
	if err != nil {
		t.Fatalf("create fulgora tables: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO datasets (source, version, collected_at, initial_input, output, croid) VALUES
			('ror', '1.63', '2024-01-01T00:00:00Z', 'input/ror', 'output/ror', ?),
			('retractionwatch', '2026-08-15', '2024-01-01T00:00:00Z', 'input/retr', 'output/retr', ?);
	`, rorCroid, retrCroid)
	if err != nil {
		t.Fatalf("insert fulgora datasets: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO latest (source, version, dataset_id) VALUES
			('ror', '1.63', 1),
			('retractionwatch', '2026-08-15', 2);
	`)
	if err != nil {
		t.Fatalf("insert fulgora latest: %v", err)
	}
}

func TestRunGeneratesRelationships(t *testing.T) {
	t.Run("all three relationship types", func(t *testing.T) {
		tmpDir := t.TempDir()

		vulcanusPath := filepath.Join(tmpDir, "vulcanus.duckdb")
		fulgoraPath := filepath.Join(tmpDir, "fulgora.db")
		nexusPath := filepath.Join(tmpDir, "nexus.db")

		vulcanusDB := openDuckDB(t, vulcanusPath)
		setupDuckDB(t, vulcanusDB)

		fulgoraDB := openSQLiteDB(t, fulgoraPath)
		setupFulgoraDB(t, fulgoraDB,
			"fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx",
			"fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx")

		nexusStore := openNexusStore(t, nexusPath)

		ctx := context.Background()
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("fetcher.Run: %v", err)
		}

		// Should have exactly 5 relationships (2 per bidirectional match, 1 for affiliation).
		all, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(all) != 5 {
			t.Fatalf("expected 5 relationships, got %d", len(all))
		}

		// Nauvis item 1 → ROR dataset (is-funded-by / funds).
		foundRorFunder := false
		foundFunds := false
		foundRorAffil := false
		foundIsRetractedBy := false
		foundRetracts := false
		for _, r := range all {
			if r.Source == "nauvis1croidxxxxxxxxxxxxxxxxxxxxxx" && r.Relationship == "is-funded-by" {
				foundRorFunder = true
				if r.Target != "fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx" {
					t.Fatalf("is-funded-by target = %q, want fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx", r.Target)
				}
			}
			if r.Source == "fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx" && r.Relationship == "funds" {
				foundFunds = true
				if r.Target != "nauvis1croidxxxxxxxxxxxxxxxxxxxxxx" {
					t.Fatalf("funds target = %q, want nauvis1croidxxxxxxxxxxxxxxxxxxxxxx", r.Target)
				}
			}
			if r.Source == "nauvis2croidxxxxxxxxxxxxxxxxxxxxxx" && r.Relationship == "ror_affiliation" {
				foundRorAffil = true
				if r.Target != "fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx" {
					t.Fatalf("ror_affiliation target = %q, want fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx", r.Target)
				}
			}
			if r.Source == "nauvis3croidxxxxxxxxxxxxxxxxxxxxxx" && r.Relationship == "is-retracted-by" {
				foundIsRetractedBy = true
				if r.Target != "fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx" {
					t.Fatalf("is-retracted-by target = %q, want fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx", r.Target)
				}
			}
			if r.Source == "fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx" && r.Relationship == "retracts" {
				foundRetracts = true
				if r.Target != "nauvis3croidxxxxxxxxxxxxxxxxxxxxxx" {
					t.Fatalf("retracts target = %q, want nauvis3croidxxxxxxxxxxxxxxxxxxxxxx", r.Target)
				}
			}
		}
		if !foundRorFunder {
			t.Fatal("missing is-funded-by relationship for nauvis item 1")
		}
		if !foundFunds {
			t.Fatal("missing funds relationship for nauvis item 1")
		}
		if !foundRorAffil {
			t.Fatal("missing ror_affiliation relationship for nauvis item 2")
		}
		if !foundIsRetractedBy {
			t.Fatal("missing is-retracted-by relationship for nauvis item 3")
		}
		if !foundRetracts {
			t.Fatal("missing retracts relationship for nauvis item 3")
		}
	})

	t.Run("idempotent — no duplicates on second run", func(t *testing.T) {
		tmpDir := t.TempDir()

		vulcanusPath := filepath.Join(tmpDir, "vulcanus.duckdb")
		fulgoraPath := filepath.Join(tmpDir, "fulgora.db")
		nexusPath := filepath.Join(tmpDir, "nexus.db")

		vulcanusDB := openDuckDB(t, vulcanusPath)
		setupDuckDB(t, vulcanusDB)

		fulgoraDB := openSQLiteDB(t, fulgoraPath)
		setupFulgoraDB(t, fulgoraDB,
			"fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx",
			"fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx")

		nexusStore := openNexusStore(t, nexusPath)

		ctx := context.Background()
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("first Run: %v", err)
		}

		firstCount, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll first: %v", err)
		}

		// Run again — should not create duplicates.
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("second Run: %v", err)
		}

		secondCount, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll second: %v", err)
		}

		if len(secondCount) != len(firstCount) {
			t.Fatalf("expected %d relationships after second run, got %d", len(firstCount), len(secondCount))
		}
	})

	t.Run("no nauvis croids — no relationships", func(t *testing.T) {
		tmpDir := t.TempDir()

		vulcanusPath := filepath.Join(tmpDir, "vulcanus.duckdb")
		fulgoraPath := filepath.Join(tmpDir, "fulgora.db")
		nexusPath := filepath.Join(tmpDir, "nexus.db")

		vulcanusDB := openDuckDB(t, vulcanusPath)
		_, err := vulcanusDB.Exec(`
			CREATE TABLE nauvis (record JSON, croid TEXT, doi TEXT);
			CREATE TABLE ror_funder_matches (paper_doi TEXT, name TEXT, ror_id TEXT, ror_name TEXT, confidence DOUBLE);
			CREATE TABLE ror_affiliation_matches (paper_doi TEXT, name TEXT, ror_id TEXT, ror_name TEXT, confidence DOUBLE);
			CREATE TABLE retractionwatch (record JSON, original_paper_doi TEXT, retraction_doi TEXT);
		`)
		if err != nil {
			t.Fatalf("create tables: %v", err)
		}
		// Tables exist but have no data.

		fulgoraDB := openSQLiteDB(t, fulgoraPath)
		setupFulgoraDB(t, fulgoraDB,
			"fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx",
			"fulgora_retr_croidxxxxxxxxxxxxxxxxxxxx")

		nexusStore := openNexusStore(t, nexusPath)

		ctx := context.Background()
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("fetcher.Run: %v", err)
		}

		all, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(all) != 0 {
			t.Fatalf("expected 0 relationships, got %d", len(all))
		}
	})

	t.Run("only ROR matching produces ror_funder and ror_affiliation", func(t *testing.T) {
		tmpDir := t.TempDir()

		vulcanusPath := filepath.Join(tmpDir, "vulcanus.duckdb")
		fulgoraPath := filepath.Join(tmpDir, "fulgora.db")
		nexusPath := filepath.Join(tmpDir, "nexus.db")

		vulcanusDB := openDuckDB(t, vulcanusPath)
		_, err := vulcanusDB.Exec(`
			CREATE TABLE nauvis (record JSON, croid TEXT, doi TEXT);
			CREATE TABLE ror_funder_matches (paper_doi TEXT, name TEXT, ror_id TEXT, ror_name TEXT, confidence DOUBLE);
			CREATE TABLE ror_affiliation_matches (paper_doi TEXT, name TEXT, ror_id TEXT, ror_name TEXT, confidence DOUBLE);
			CREATE TABLE retractionwatch (record JSON, original_paper_doi TEXT, retraction_doi TEXT);
		`)
		if err != nil {
			t.Fatalf("create tables: %v", err)
		}
		_, err = vulcanusDB.Exec(`
			INSERT INTO nauvis (croid, doi, record) VALUES ('nauvis1croidxxxxxxxxxxxxxxxxxxxxxx', '10.1/test', '{}');
			INSERT INTO ror_funder_matches (paper_doi, name, ror_id, ror_name, confidence) VALUES ('10.1/test', 'NSF', '01', 'NSO', 98.0);
		`)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}

		// Fulgora has ROR dataset but no retraction dataset.
		fulgoraDB := openSQLiteDB(t, fulgoraPath)
		_, err = fulgoraDB.Exec(`
			CREATE TABLE datasets (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				source TEXT NOT NULL,
				version TEXT NOT NULL,
				collected_at TEXT NOT NULL,
				initial_input TEXT NOT NULL,
				output TEXT NOT NULL,
				croid TEXT,
				UNIQUE (source, version)
			);
			CREATE TABLE latest (
				source TEXT PRIMARY KEY,
				version TEXT NOT NULL,
				dataset_id INTEGER NOT NULL REFERENCES datasets (id)
			);
		`)
		if err != nil {
			t.Fatalf("create fulgora tables: %v", err)
		}
		_, err = fulgoraDB.Exec(`
			INSERT INTO datasets (source, version, collected_at, initial_input, output, croid) VALUES
				('ror', '1.63', '2024-01-01T00:00:00Z', 'input/ror', 'output/ror', 'fulgora_ror_croidxxxxxxxxxxxxxxxxxxxx');
			INSERT INTO latest (source, version, dataset_id) VALUES ('ror', '1.63', 1);
		`)
		if err != nil {
			t.Fatalf("insert fulgora: %v", err)
		}

		nexusStore := openNexusStore(t, nexusPath)

		ctx := context.Background()
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("fetcher.Run: %v", err)
		}

		all, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("expected 2 relationships, got %d", len(all))
		}
		relationships := []string{all[0].Relationship, all[1].Relationship}
		if all[0].Relationship != "is-funded-by" && all[1].Relationship != "is-funded-by" {
			t.Fatalf("missing is-funded-by, got %v", relationships)
		}
		if all[0].Relationship != "funds" && all[1].Relationship != "funds" {
			t.Fatalf("missing funds, got %v", relationships)
		}
	})

	t.Run("OFR funder matching produces ofr_funded_by relationship", func(t *testing.T) {
		tmpDir := t.TempDir()

		vulcanusPath := filepath.Join(tmpDir, "vulcanus.duckdb")
		fulgoraPath := filepath.Join(tmpDir, "fulgora.db")
		nexusPath := filepath.Join(tmpDir, "nexus.db")

		vulcanusDB := openDuckDB(t, vulcanusPath)
		_, err := vulcanusDB.Exec(`
			CREATE TABLE nauvis (record JSON, croid TEXT, doi TEXT);
			CREATE TABLE ofr_matches (nauvis_item_doi TEXT, funder_name TEXT, funder_uri TEXT, funder_match TEXT, score DOUBLE);
		`)
		if err != nil {
			t.Fatalf("create tables: %v", err)
		}
		_, err = vulcanusDB.Exec(`
			INSERT INTO nauvis (croid, doi, record) VALUES ('nauvis1croidxxxxxxxxxxxxxxxxxxxxxx', '10.1/test', '{}');
			INSERT INTO ofr_matches (nauvis_item_doi, funder_name, funder_uri, funder_match, score) VALUES ('10.1/test', 'NSF', 'https://dx.doi.org/10.13039/00000001', 'NSF', 95.0);
		`)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}

		fulgoraDB := openSQLiteDB(t, fulgoraPath)
		_, err = fulgoraDB.Exec(`
			CREATE TABLE datasets (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				source TEXT NOT NULL,
				version TEXT NOT NULL,
				collected_at TEXT NOT NULL,
				initial_input TEXT NOT NULL,
				output TEXT NOT NULL,
				croid TEXT,
				UNIQUE (source, version)
			);
			CREATE TABLE latest (
				source TEXT PRIMARY KEY,
				version TEXT NOT NULL,
				dataset_id INTEGER NOT NULL REFERENCES datasets (id)
			);
		`)
		if err != nil {
			t.Fatalf("create fulgora tables: %v", err)
		}
		_, err = fulgoraDB.Exec(`
			INSERT INTO datasets (source, version, collected_at, initial_input, output, croid) VALUES
				('openfunderregistry', '2026-10-13', '2024-01-01T00:00:00Z', 'input/ofr', 'output/ofr', 'fulgora_ofr_croidxxxxxxxxxxxxxxxxxxxx');
			INSERT INTO latest (source, version, dataset_id) VALUES ('openfunderregistry', '2026-10-13', 1);
		`)
		if err != nil {
			t.Fatalf("insert fulgora: %v", err)
		}

		nexusStore := openNexusStore(t, nexusPath)

		ctx := context.Background()
		if err := fetcher.Run(ctx, vulcanusPath, fulgoraPath, nexusStore, nil); err != nil {
			t.Fatalf("fetcher.Run: %v", err)
		}

		all, err := nexusStore.ListAll(ctx)
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("expected 2 relationships (is-funded-by + funds), got %d", len(all))
		}

		// Verify both relationships exist.
		rels := map[string]bool{all[0].Relationship: true, all[1].Relationship: true}
		if !rels["is-funded-by"] || !rels["funds"] {
			t.Fatalf("expected is-funded-by and funds relationships, got %v", rels)
		}

		// Verify is-funded-by direction.
		foundForward := false
		foundReverse := false
		for _, r := range all {
			if r.Relationship == "is-funded-by" && r.Source == "nauvis1croidxxxxxxxxxxxxxxxxxxxxxx" && r.Target == "fulgora_ofr_croidxxxxxxxxxxxxxxxxxxxx" {
				foundForward = true
			}
			if r.Relationship == "funds" && r.Source == "fulgora_ofr_croidxxxxxxxxxxxxxxxxxxxx" && r.Target == "nauvis1croidxxxxxxxxxxxxxxxxxxxxxx" {
				foundReverse = true
			}
		}
		if !foundForward {
			t.Fatal("missing is-funded-by relationship for nauvis item 1")
		}
		if !foundReverse {
			t.Fatal("missing funds relationship for OFR dataset")
		}
	})
}
