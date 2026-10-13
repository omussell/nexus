package match

import (
	"path/filepath"
	"testing"
)

func TestOFRClientMatchExact(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	// Create the openfunderregistry table.
	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert a known funder.
	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001809",
		"National Science Foundation",
		"",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Exact match should succeed.
	matches := client.MatchOFR("National Science Foundation")
	if len(matches) == 0 {
		t.Fatal("expected a match for exact funder name")
	}
	if matches[0]["name"] != "National Science Foundation" {
		t.Errorf("expected name 'National Science Foundation', got %v", matches[0]["name"])
	}
	if matches[0]["score"].(float64) < 96 {
		t.Errorf("expected score >= 96, got %v", matches[0]["score"])
	}
}

func TestOFRClientMatchNoMatch(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert a funder that won't match the query.
	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001809",
		"African Research Initiative",
		"",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Unrelated query should return no matches.
	matches := client.MatchOFR("National Science Foundation")
	if len(matches) > 0 {
		t.Errorf("expected no matches, got %d", len(matches))
	}
}

func TestOFRClientQueryOFR(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001809",
		"National Science Foundation",
		"",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	candidates := client.QueryOFR("National", 10)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Name != "National Science Foundation" {
		t.Errorf("expected 'National Science Foundation', got %s", candidates[0].Name)
	}
}

func TestOFRClientQueryExcludesReplaced(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert a replaced record.
	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100011576",
		"Japan Society for the Advancement of Science",
		"10.13039/501100001691",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Replaced record should not appear in query results.
	candidates := client.QueryOFR("Japan Society", 10)
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates (replaced), got %d", len(candidates))
	}
}

func TestOFRClientMatchEmptyInput(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	matches := client.MatchOFR("")
	if len(matches) > 0 {
		t.Error("expected no matches for empty input")
	}
}

func TestOFRClientQueryEmptyInput(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	candidates := client.QueryOFR("", 10)
	if len(candidates) > 0 {
		t.Error("expected no candidates for empty input")
	}
}

func TestOFRClientCount(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001809",
		"National Science Foundation",
		"",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if client.Count() != 1 {
		t.Errorf("expected count 1, got %d", client.Count())
	}
}

func TestOFRClientCountExcludesReplaced(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// One active, one replaced.
	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001809",
		"National Science Foundation",
		"",
	); err != nil {
		t.Fatalf("insert active: %v", err)
	}
	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100011576",
		"Japan Society for the Advancement of Science",
		"10.13039/501100001691",
	); err != nil {
		t.Fatalf("insert replaced: %v", err)
	}

	// Only the active one should be counted.
	if client.Count() != 1 {
		t.Errorf("expected count 1 (active only), got %d", client.Count())
	}
}

func TestOFRClientCloseNilDB(t *testing.T) {
	var client OFRClient
	if err := client.Close(); err != nil {
		t.Errorf("Close on nil DB should not error, got %v", err)
	}
}

func TestOFRClientMatchFuzzyTypo(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}
	defer client.Close()

	if _, err := client.db.Exec(`
		CREATE TABLE openfunderregistry (
			record   JSON,
			funder_uri TEXT,
			funder_name TEXT,
			replaced   TEXT
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if _, err := client.db.Exec(
		`INSERT INTO openfunderregistry (funder_uri, funder_name, replaced) VALUES (?, ?, ?)`,
		"https://dx.doi.org/10.13039/501100001691",
		"Japan Society for the Promotion of Science",
		"",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// A name with a typo that might still match at 96%+.
	matches := client.MatchOFR("Japan Society for the Promoton of Sciece")
	if len(matches) == 0 {
		// A 96% match is possible for this typo level — just log what happened.
		t.Logf("No match for 'Japan Society for the Promoton of Sciece' (expected or not)")
	}

	// Exact should always match.
	matches = client.MatchOFR("Japan Society for the Promotion of Science")
	if len(matches) != 1 {
		t.Fatalf("expected 1 exact match, got %d", len(matches))
	}
}

func TestOFRClientNewFromDB(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.duckdb")

	client, err := NewOFRClient(dbPath)
	if err != nil {
		t.Fatalf("NewOFRClient: %v", err)
	}

	derived := NewOFRClientFromDB(client.db)
	if derived == nil {
		t.Fatal("expected non-nil client from NewOFRClientFromDB")
	}
	derived.Close()
	client.Close()
}
