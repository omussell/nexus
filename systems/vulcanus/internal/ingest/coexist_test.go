package ingest

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// TestNauvisAndFulgoraCoexist ingests both providers into the same DuckDB
// database — possibly at different times — and confirms both the single
// `nauvis` table and each Fulgora source table are present and uncorrupted.
func TestNauvisAndFulgoraCoexist(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/a"}`})
	nStore := openStore(t, map[string][]string{
		"0.ndjson": {"10.1/a"},
	})

	fDir := t.TempDir()
	fSources := []string{"ror", "retractionwatch"}
	fStore := openFulgoraStore(t, fSources, fDir)

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")

	// Simulate importing Nauvis now and Fulgora later (two separate runs).
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}

	db := freshDB(t, outDB)

	if got := countRows(t, db, "nauvis"); got != 1 {
		t.Fatalf("nauvis rows = %d, want 1", got)
	}
	for _, name := range fSources {
		if got := countRows(t, db, name); got != 1 {
			t.Fatalf("table %s rows = %d, want 1", name, got)
		}
	}

	want := append([]string{"nauvis"}, fSources...)
	sort.Strings(want)

	got := append([]string{}, tableNames(t, db)...)
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tables = %v, want %v", got, want)
		}
	}

	// Re-running either provider replaces its own tables without touching the
	// other provider's.
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora re-Run: %v", err)
	}
	db = freshDB(t, outDB)
	if got := countRows(t, db, "nauvis"); got != 1 {
		t.Fatalf("nauvis rows after fulgora re-run = %d, want 1", got)
	}
	if got := countRows(t, db, "ror"); got != 1 {
		t.Fatalf("ror rows after re-run = %d, want 1", got)
	}

	// A corrupt file in one provider must not affect the other provider's table.
	visDir2 := t.TempDir()
	writeNDJSON(t, visDir2, "0.ndjson", []string{`{"DOI":"10.1/a"}`, "garbage"})
	nStore2 := openStore(t, map[string][]string{"0.ndjson": {"10.1/a"}})
	outDB2 := filepath.Join(t.TempDir(), "vulcanus2.duckdb")
	if n, err := Run(ctx, fStore, fDir, outDB2); err != nil || n != 2 {
		t.Fatalf("fulgora Run into fresh db: n=%d err=%v, want 2 nil", n, err)
	}
	if _, err := Run(ctx, nStore2, visDir2, outDB2); err == nil {
		t.Fatalf("expected nauvis Run to fail on garbage line")
	}
	// The fulgora tables must survive the nauvis rollback intact.
	db = freshDB(t, outDB2)
	if got := countRows(t, db, "ror"); got != 1 {
		t.Fatalf("ror rows after nauvis failure = %d, want 1", got)
	}
	if got := countRows(t, db, "retractionwatch"); got != 1 {
		t.Fatalf("retractionwatch rows after nauvis failure = %d, want 1", got)
	}
}
