package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nexus/vulcanus/internal/fulgora"
)

// openFulgoraStoreWithLines seeds a fulgora registry with one latest dataset
// per source and writes the given NDJSON lines to each source's output path.
// Returns the store and the data root under which the files were written, so
// a caller can pass that same root as Run's outDir (Run resolves each
// Source.OutputPath against it).
func openFulgoraStoreWithLines(t *testing.T, lines map[string][]string) (*fulgora.Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fulgora.sqlite3")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(`CREATE TABLE datasets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source TEXT NOT NULL, version TEXT NOT NULL,
		collected_at TEXT NOT NULL, initial_input TEXT NOT NULL,
		output TEXT NOT NULL, UNIQUE (source, version))`); err != nil {
		t.Fatalf("create datasets: %v", err)
	}
	if _, err := conn.Exec(`CREATE TABLE latest (
		source TEXT PRIMARY KEY, version TEXT NOT NULL,
		dataset_id INTEGER NOT NULL REFERENCES datasets (id))`); err != nil {
		t.Fatalf("create latest: %v", err)
	}

	names := make([]string, 0, len(lines))
	for name := range lines {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		outRel := filepath.Join(name, "output", name+"-1.0.json")
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(outRel)), 0o755); err != nil {
			t.Fatalf("mkdir output for %s: %v", name, err)
		}
		var body []byte
		for _, l := range lines[name] {
			body = append(body, l...)
			body = append(body, '\n')
		}
		if err := os.WriteFile(filepath.Join(dir, outRel), body, 0o644); err != nil {
			t.Fatalf("write output for %s: %v", name, err)
		}
		res, err := conn.Exec(`INSERT INTO datasets
			(source, version, collected_at, initial_input, output)
		 VALUES (?, '1.0', '2026-01-01T00:00:00Z', ?, ?)`,
			name, filepath.Join(name, "initial_input", name+"-in.csv"), outRel)
		if err != nil {
			t.Fatalf("insert dataset %s: %v", name, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last id for %s: %v", name, err)
		}
		if _, err := conn.Exec(`INSERT INTO latest
			(source, version, dataset_id) VALUES (?, '1.0', ?)`, name, id); err != nil {
			t.Fatalf("insert latest %s: %v", name, err)
		}
	}

	store, err := fulgora.Open(dbPath, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

// viewExists reports whether a view named name is present in the main schema.
func viewExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(`
		SELECT table_name FROM information_schema.views
		 WHERE table_schema = 'main' AND table_name = ?`, name).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("query view %s: %v", name, err)
	}
	return true
}

func TestRetractionsView_AppearsOnlyWhenBothTablesExist(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/matched"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/matched"}})
	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}

	if viewExists(t, freshDB(t, outDB), retractionsViewName) {
		t.Fatalf("retractions view present after nauvis-only ingest; want absent")
	}

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/matched","RetractionDOI":"10.1/matched"}`},
	})
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}
	if !viewExists(t, freshDB(t, outDB), retractionsViewName) {
		t.Fatalf("retractions view missing after both ingests")
	}
}

func TestRetractionsView_MatchesByDOI(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{
		`{"DOI":"10.1/yes","title":"N matched"}`,
		`{"DOI":"10.1/nope","title":"N unmatched"}`,
	})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes", "10.1/nope"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/no"}`},
	})

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}

	db := freshDB(t, outDB)
	if got := countRows(t, db, retractionsViewName); got != 1 {
		t.Fatalf("retractions rows = %d, want 1", got)
	}

	rows, err := db.Query(`SELECT nauvis_record, retractionwatch_record, matched_doi
		FROM ` + retractionsViewName + ` ORDER BY matched_doi`)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatalf("expected one row in view")
	}
	var nauVis, retraction interface{}
	var matched sql.NullString
	if err := rows.Scan(&nauVis, &retraction, &matched); err != nil {
		t.Fatalf("scan view row: %v", err)
	}
	nb, err := json.Marshal(nauVis)
	if err != nil {
		t.Fatalf("re-marshal nauvis_record: %v", err)
	}
	rb, err := json.Marshal(retraction)
	if err != nil {
		t.Fatalf("re-marshal retractionwatch_record: %v", err)
	}
	var nv, rw map[string]any
	if err := json.Unmarshal(nb, &nv); err != nil {
		t.Fatalf("view nauvis_record not a JSON object: %v", err)
	}
	if err := json.Unmarshal(rb, &rw); err != nil {
		t.Fatalf("view retractionwatch_record not a JSON object: %v", err)
	}
	if nv["DOI"] != "10.1/yes" {
		t.Fatalf("view nauvis_record DOI = %v, want 10.1/yes", nv["DOI"])
	}
	if nv["title"] != "N matched" {
		t.Fatalf("view nauvis_record title = %v, want 'N matched'", nv["title"])
	}
	if rw["OriginalPaperDOI"] != "10.1/yes" {
		t.Fatalf("view retractionwatch_record OriginalPaperDOI = %v, want 10.1/yes", rw["OriginalPaperDOI"])
	}
	if rw["RetractionDOI"] != "10.1/no" {
		t.Fatalf("view retractionwatch_record RetractionDOI = %v, want 10.1/no", rw["RetractionDOI"])
	}
	if matched.String != "10.1/yes" || !matched.Valid {
		t.Fatalf("view matched_doi = %q (valid=%v), want 10.1/yes", matched.String, matched.Valid)
	}
	if rows.Next() {
		t.Fatalf("view returned more than one row")
	}
}

func TestRetractionsView_IdempotentAcrossReRuns(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/yes"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes"}})
	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/yes"}`},
	})

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run 1: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run 1: %v", err)
	}
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run 2: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run 2: %v", err)
	}

	db := freshDB(t, outDB)
	if !viewExists(t, db, retractionsViewName) {
		t.Fatalf("retractions view missing after re-runs")
	}
	if got := countRows(t, db, retractionsViewName); got != 1 {
		t.Fatalf("retractions rows after re-runs = %d, want 1", got)
	}
}

func TestRetractionsView_MultipleMatches(t *testing.T) {
	ctx := context.Background()

	// One Nauvis DOI, two retraction notices pointing at it — the view should
	// expose both pairs.
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/yes"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc1","Title":"First notice"}`,
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc2","Title":"Second notice"}`,
		},
	})

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}
	db := freshDB(t, outDB)
	if got := countRows(t, db, retractionsViewName); got != 2 {
		t.Fatalf("retractions rows = %d, want 2 (one per retraction notice)", got)
	}
}

func TestRetractionsView_NoMatchYieldsEmptyView(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/different"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/different"}})
	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/other"}`},
	})

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}
	db := freshDB(t, outDB)
	if !viewExists(t, db, retractionsViewName) {
		t.Fatalf("retractions view missing though both tables exist")
	}
	if got := countRows(t, db, retractionsViewName); got != 0 {
		t.Fatalf("retractions rows = %d, want 0 (no matching DOI)", got)
	}
}
