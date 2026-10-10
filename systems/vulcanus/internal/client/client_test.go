package client

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vulcanus.duckdb")

	c, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return c
}

func createSchema(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()

	_, err := c.db.ExecContext(ctx, `
		CREATE TABLE nauvis (
			record JSON,
			doi TEXT,
			funder_names JSON,
			affiliation_names JSON
		)
	`)
	if err != nil {
		t.Fatalf("create nauvis table: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE TABLE ror (
			record JSON,
			ror_id TEXT,
			country TEXT,
			status TEXT,
			names JSON
		)
	`)
	if err != nil {
		t.Fatalf("create ror table: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE TABLE retractionwatch (
			record JSON,
			original_paper_doi TEXT,
			retraction_doi TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create retractionwatch table: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE TABLE ror_funder_matches (
			paper_doi TEXT,
			name TEXT,
			ror_id TEXT,
			ror_name TEXT,
			confidence DOUBLE
		)
	`)
	if err != nil {
		t.Fatalf("create ror_funder_matches table: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE TABLE ror_affiliation_matches (
			paper_doi TEXT,
			name TEXT,
			ror_id TEXT,
			ror_name TEXT,
			confidence DOUBLE
		)
	`)
	if err != nil {
		t.Fatalf("create ror_affiliation_matches table: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE VIEW retractions AS
		SELECT
			n.record AS nauvis_record,
			r.record AS retractionwatch_record,
			r.retraction_doi AS retraction_notice_doi
		FROM nauvis n
		JOIN retractionwatch r ON n.doi = r.original_paper_doi
	`)
	if err != nil {
		t.Fatalf("create retractions view: %v", err)
	}

	_, err = c.db.ExecContext(ctx, `
		CREATE VIEW retraction_provenance AS
		SELECT
			n.record AS nauvis_record,
			r.record AS retractionwatch_record,
			n.doi AS paper_doi,
			r.retraction_doi AS retraction_notice_doi,
			'paper->retraction:' || n.doi || '|retraction->paper:' || r.retraction_doi AS provenance
		FROM nauvis n
		JOIN retractionwatch r ON n.doi = r.original_paper_doi
	`)
	if err != nil {
		t.Fatalf("create retraction_provenance view: %v", err)
	}
}

func insertTestData(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()

	// Insert nauvis records.
	nauvis1 := `{"DOI":"10.1.0.1/test1","title":"Test Paper 1","funder":[{"name":"National Science Foundation"}],"author":[{"name":"Jane Doe","affiliation":[{"name":"MIT"}]}]}`
	nauvis2 := `{"DOI":"10.1.0.1/test2","title":"Test Paper 2","funder":[{"name":"DNRF"}],"author":[{"name":"John Smith","affiliation":[{"name":"University of Copenhagen"}]}]}`
	nauvis3 := `{"DOI":"10.1.0.1/test3","title":"Test Paper 3","funder":[],"author":[]}`

	for _, jsonStr := range []string{nauvis1, nauvis2, nauvis3} {
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &rec); err != nil {
			t.Fatalf("unmarshal nauvis: %v", err)
		}
		doi := rec["DOI"].(string)
		_ = rec["funder"]
		_ = rec["author"]

		_, err := c.db.ExecContext(ctx, `
			INSERT INTO nauvis (record, doi, funder_names, affiliation_names)
			VALUES (?, ?, json_extract(?, '$.funder[*].name'), json_extract(?, '$.author[*].affiliation[*].name'))
		`, jsonStr, doi, jsonStr, jsonStr)
		if err != nil {
			t.Fatalf("insert nauvis: %v", err)
		}
	}

	// Insert ROR orgs.
	rorOrgs := []struct {
		id   string
		name string
		country string
		status  string
	}{
		{"01abc", "National Science Foundation", "US", "active"},
		{"02def", "University of Copenhagen", "DK", "active"},
		{"03ghi", "DNRF - Danish National Research Foundation", "DK", "active"},
		{"04jkl", "Withdrawn Org", "US", "withdrawn"},
	}

	for _, o := range rorOrgs {
		namesJSON, _ := json.Marshal([]string{o.name})
		record, _ := json.Marshal(map[string]interface{}{
			"id":       o.id,
			"name":     o.name,
			"country":  o.country,
			"status":   o.status,
			"names":    []map[string]string{{"value": o.name}},
		})
		_, err := c.db.ExecContext(ctx, `
			INSERT INTO ror (record, ror_id, country, status, names)
			VALUES (?, ?, ?, ?, ?)
		`, string(record), o.id, o.country, o.status, string(namesJSON))
		if err != nil {
			t.Fatalf("insert ror: %v", err)
		}
	}

	// Insert retraction watch records.
	retraction1 := `{"original_paper_doi":"10.1.0.1/test1","retraction_doi":"10.1.0.1/ret1"}`
	_, err := c.db.ExecContext(ctx, `
		INSERT INTO retractionwatch (record, original_paper_doi, retraction_doi)
		VALUES (?, '10.1.0.1/test1', '10.1.0.1/ret1')
	`, retraction1)
	if err != nil {
		t.Fatalf("insert retraction: %v", err)
	}

	// Insert funder matches.
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO ror_funder_matches (paper_doi, name, ror_id, ror_name, confidence)
		VALUES ('10.1.0.1/test1', 'National Science Foundation', '01abc', 'National Science Foundation', 100.0)
	`)
	if err != nil {
		t.Fatalf("insert funder match: %v", err)
	}

	// Insert affiliation matches.
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO ror_affiliation_matches (paper_doi, name, ror_id, ror_name, confidence)
		VALUES ('10.1.0.1/test1', 'MIT', '01abc', 'National Science Foundation', 85.0)
	`)
	if err != nil {
		t.Fatalf("insert affiliation match: %v", err)
	}
}

func TestOpen_Close(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	ctx := context.Background()
	// Should work even on empty db.
	tables, err := c.Tables(ctx)
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	_ = tables
}

func TestCountNauvis_Empty(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)

	ctx := context.Background()
	n, err := c.CountNauvis(ctx)
	if err != nil {
		t.Fatalf("CountNauvis: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestNauvis(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	// Count
	n, err := c.CountNauvis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("CountNauvis = %d, want 3", n)
	}

	// List DOIs
	dois, err := c.ListNauvisDOIs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dois) != 3 {
		t.Errorf("ListNauvisDOIs = %d, want 3", len(dois))
	}

	// ByDOI
	rec, err := c.NauvisByDOI(ctx, "10.1.0.1/test1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.DOI != "10.1.0.1/test1" {
		t.Errorf("DOI = %q, want %q", rec.DOI, "10.1.0.1/test1")
	}
	if len(rec.FunderNames) != 1 || rec.FunderNames[0] != "National Science Foundation" {
		t.Errorf("FunderNames = %v, want [National Science Foundation]", rec.FunderNames)
	}
	if len(rec.AffiliationNames) != 1 || rec.AffiliationNames[0] != "MIT" {
		t.Errorf("AffiliationNames = %v, want [MIT]", rec.AffiliationNames)
	}

	// Not found
	_, err = c.NauvisByDOI(ctx, "10.1.0.1/nonexistent")
	if err != nil && err.Error() != "no rows in result set" {
		// DuckDB returns different error for no rows
		t.Logf("NauvisByDOI nonexistent: %v", err)
	}

	// QueryNauvis with limit
	records, err := c.QueryNauvis(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Errorf("QueryNauvis(2) = %d, want 2", len(records))
	}
}

func TestROR(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	n, err := c.CountROR(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("CountROR = %d, want 4", n)
	}

	// ByID
	rec, err := c.RORByID(ctx, "01abc")
	if err != nil {
		t.Fatal(err)
	}
	if rec.RORID != "01abc" {
		t.Errorf("RORID = %q, want %q", rec.RORID, "01abc")
	}
	if rec.Country != "US" {
		t.Errorf("Country = %q, want %q", rec.Country, "US")
	}
	if rec.Status != "active" {
		t.Errorf("Status = %q, want %q", rec.Status, "active")
	}

	// Search
	orgs, err := c.QueryROR(ctx, "science", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].RORID != "01abc" {
		t.Errorf("QueryROR = %v, want [01abc]", orgs)
	}

	// Search with country filter
	orgs, err = c.QueryROR(ctx, "Copenhagen", "DK", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].RORID != "02def" {
		t.Errorf("QueryROR with country = %v, want [02def]", orgs)
	}

	// Withdrawn orgs should not appear in search
	orgs, err = c.QueryROR(ctx, "Withdrawn", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 0 {
		t.Errorf("QueryROR for withdrawn = %d, want 0", len(orgs))
	}
}

func TestRetractions(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	// Count
	n, err := c.CountRetractions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("CountRetractions = %d, want 1", n)
	}

	// ByDOI
	records, err := c.RetractionsByDOI(ctx, "10.1.0.1/test1")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Errorf("RetractionsByDOI = %d, want 1", len(records))
	}
	if records[0].RetractionDOI != "10.1.0.1/ret1" {
		t.Errorf("RetractionDOI = %q, want %q", records[0].RetractionDOI, "10.1.0.1/ret1")
	}

	// No matches
	records, err = c.RetractionsByDOI(ctx, "10.1.0.1/test2")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("RetractionsByDOI(test2) = %d, want 0", len(records))
	}

	// Query all
	all, err := c.QueryRetractions(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("QueryRetractions = %d, want 1", len(all))
	}
}

func TestMatches(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	// Funder matches
	fCount, err := c.CountFunderMatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fCount != 1 {
		t.Errorf("CountFunderMatches = %d, want 1", fCount)
	}

	fMatches, err := c.FunderMatchesByDOI(ctx, "10.1.0.1/test1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fMatches) != 1 {
		t.Errorf("FunderMatchesByDOI = %d, want 1", len(fMatches))
	}
	if fMatches[0].RORID != "01abc" || fMatches[0].Confidence != 100.0 {
		t.Errorf("FunderMatch = %+v, want RORID=01abc Confidence=100.0", fMatches[0])
	}

	// Affiliation matches
	aCount, err := c.CountAffiliationMatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if aCount != 1 {
		t.Errorf("CountAffiliationMatches = %d, want 1", aCount)
	}

	aMatches, err := c.AffiliationMatchesByDOI(ctx, "10.1.0.1/test1")
	if err != nil {
		t.Fatal(err)
	}
	if len(aMatches) != 1 {
		t.Errorf("AffiliationMatchesByDOI = %d, want 1", len(aMatches))
	}
	if aMatches[0].RORID != "01abc" || aMatches[0].Confidence != 85.0 {
		t.Errorf("AffiliationMatch = %+v, want RORID=01abc Confidence=85.0", aMatches[0])
	}
}

func TestViews(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	// Retractions view
	retracts, err := c.QueryRetractionsView(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(retracts) != 1 {
		t.Errorf("QueryRetractionsView = %d, want 1", len(retracts))
	}
	if retracts[0].RetractionNoticeDOI != "10.1.0.1/ret1" {
		t.Errorf("RetractionNoticeDOI = %q, want %q", retracts[0].RetractionNoticeDOI, "10.1.0.1/ret1")
	}

	// Provenance view
	prov, err := c.QueryProvenance(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prov) != 1 {
		t.Errorf("QueryProvenance = %d, want 1", len(prov))
	}
	if prov[0].PaperDOI != "10.1.0.1/test1" {
		t.Errorf("PaperDOI = %q, want %q", prov[0].PaperDOI, "10.1.0.1/test1")
	}
}

func TestTables(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()
	tables, err := c.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"nauvis", "retractionwatch", "ror", "ror_affiliation_matches", "ror_funder_matches"}
	if len(tables) != len(expected) {
		t.Errorf("Tables = %d, want %d", len(tables), len(expected))
	}

	for _, exp := range expected {
		found := false
		for _, t2 := range tables {
			if t2 == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Tables missing %q", exp)
		}
	}
}

func TestClient_Count(t *testing.T) {
	c := setupTestDB(t)
	defer c.Close()

	createSchema(t, c)
	insertTestData(t, c)

	ctx := context.Background()

	for _, table := range []string{"nauvis", "ror", "retractionwatch", "ror_funder_matches", "ror_affiliation_matches"} {
		n, err := c.Count(ctx, table)
		if err != nil {
			t.Errorf("Count(%q): %v", table, err)
			continue
		}
		t.Logf("Count(%q) = %d", table, n)
	}
}

func TestNewFromDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.duckdb")

	c, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c2 := NewFromDB(c.db)
	defer c2.Close()

	ctx := context.Background()
	tables, err := c2.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tables
}


