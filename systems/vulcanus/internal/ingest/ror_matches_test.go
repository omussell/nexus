package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// copyResources copies the resources directory to dst/resources (including subdirs).
func copyResources(t *testing.T, dst string) {
	t.Helper()
	// Walk up to find the vulcanus module root, then go to resources.
	cwd, _ := os.Getwd()
	root := cwd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	src := filepath.Join(root, "internal", "match", "resources")
	dst = filepath.Join(dst, "resources")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("mkdir resources dir: %v", err)
	}
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		dest := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, _ := os.ReadFile(path)
		return os.WriteFile(dest, data, 0644)
	}); err != nil {
		t.Fatalf("copy resources: %v", err)
	}
}

// TestRORMatches runs ROR matching against a minimal Nauvis+ROR dataset and
// verifies the result tables contain the expected rows.
func TestRORMatches(t *testing.T) {
	ctx := context.Background()

	// --- set up nauvis store with one record that has funder names ---
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{
		`{"DOI":"10.1/test","funder":[{"name":"National Science Foundation"}]}`,
	})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/test"}})

	// --- set up fulgora store with ROR and retractionwatch sources ---
	fDir := t.TempDir()
	fSources := []string{"ror", "retractionwatch"}
	fStore := openFulgoraStore(t, fSources, fDir)

	outDir := t.TempDir()
	outDB := filepath.Join(outDir, "vulcanus.duckdb")

	// Seed ROR data.
	rorFile := filepath.Join(fDir, "ror", "output", "ror-1.0.json")
	writeNDJSON(t, "", rorFile, []string{
		`{"id":"https://ror.org/0220q3k92","status":"active","country":"US","primary":{"name":"National Science Foundation"},"names":[{"name":"National Science Foundation"}]}`,
		`{"id":"https://ror.org/025vr7951","status":"active","country":"US","primary":{"name":"MIT"},"names":[{"name":"Massachusetts Institute of Technology"},{"name":"MIT"}]}`,
	})

	// Seed retractionwatch data.
	rwFile := filepath.Join(fDir, "retractionwatch", "retractions.json")
	writeNDJSON(t, "", rwFile, []string{
		`{"RetractionDOI":"10.1/rw1","OriginalPaperDOI":"10.1/rw1"}`,
	})

	// Copy country resources so RunRORMatches can find them (looks relative to DB dir).
	copyResources(t, outDir)

	// Ingest both sources.
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}

	// Run ROR matching.
	if err := RunRORMatches(ctx, outDB); err != nil {
		t.Fatalf("RunRORMatches: %v", err)
	}

	// Verify result tables.
	db := freshDB(t, outDB)

	// Should have the nauvis and ror tables plus the result tables.
	tables := tableNames(t, db)
	wantTables := []string{"nauvis", "retractionwatch", "ror", "ror_funder_matches", "ror_affiliation_matches"}
	for _, w := range wantTables {
		found := false
		for _, t := range tables {
			if t == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected table %q, not found in %v", w, tables)
		}
	}

	// The funder name "National Science Foundation" should match the ROR org.
	n, err := db.QueryContext(ctx, `SELECT COUNT(*) FROM ror_funder_matches`)
	if err != nil {
		t.Fatalf("query ror_funder_matches: %v", err)
	}
	defer n.Close()
	if !n.Next() {
		t.Fatal("ror_funder_matches has no rows")
	}
	var funderCount int
	if err := n.Scan(&funderCount); err != nil {
		t.Fatalf("scan funder count: %v", err)
	}
	if funderCount != 1 {
		t.Fatalf("ror_funder_matches count = %d, want 1", funderCount)
	}

	// Check the matched ROR ID.
	row2 := db.QueryRowContext(ctx, `SELECT ror_id FROM ror_funder_matches`)
	var rorID string
	if err := row2.Scan(&rorID); err != nil {
		t.Fatalf("scan ror_id: %v", err)
	}
	if rorID != "https://ror.org/0220q3k92" {
		t.Fatalf("ror_id = %q, want %q", rorID, "https://ror.org/0220q3k92")
	}
}

// TestRORMatchesNoOp verifies RunRORMatches is a no-op when ror table is absent.
func TestRORMatchesNoOp(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{
		`{"DOI":"10.1/test","funder":[{"name":"Some Funder"}]}`,
	})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/test"}})

	fDir := t.TempDir()
	fStore := openFulgoraStore(t, []string{"retractionwatch"}, fDir)
	rwFile := filepath.Join(fDir, "retractionwatch", "retractions.json")
	writeNDJSON(t, "", rwFile, []string{
		`{"RetractionDOI":"10.1/rw1","OriginalPaperDOI":"10.1/rw1"}`,
	})

	outDir := t.TempDir()
	outDB := filepath.Join(outDir, "vulcanus.duckdb")

	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}

	// RunRORMatches should be a no-op since there's no ror table.
	if err := RunRORMatches(ctx, outDB); err != nil {
		t.Fatalf("RunRORMatches (no ror table): %v", err)
	}

	db := freshDB(t, outDB)
	tables := tableNames(t, db)
	for _, tname := range tables {
		if tname == "ror_funder_matches" {
			t.Fatal("ror_funder_matches should not exist when ror table is absent")
		}
	}
}
