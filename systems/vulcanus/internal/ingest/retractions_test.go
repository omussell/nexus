package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func TestRetractionProvenance_Bidirectional_ColumnValues(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/paper","title":"N matched"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/paper"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/paper","RetractionDOI":"10.1/rc","title":"Retraction notice"}`},
	})

	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}

	db := freshDB(t, outDB)

	rows, err := db.Query(`SELECT provenance, paper_doi, retraction_doi FROM ` + retractionProvenanceViewName + ` ORDER BY provenance`)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()

	var passiveRow, activeRow struct {
		provenance    string
		paperDOI      string
		retractionDOI string
	}
	n := 0
	for rows.Next() {
		var p, pdf, rdf string
		if err := rows.Scan(&p, &pdf, &rdf); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		n++
		if strings.Contains(p, "is-retracted-by") {
			passiveRow = struct {
				provenance    string
				paperDOI      string
				retractionDOI string
			}{p, pdf, rdf}
		} else if strings.Contains(p, " retracts ") {
			activeRow = struct {
				provenance    string
				paperDOI      string
				retractionDOI string
			}{p, pdf, rdf}
		}
	}

	if n != 2 {
		t.Fatalf("expected 2 rows, got %d", n)
	}

	passiveWant := "10.1/paper is-retracted-by 10.1/rc"
	activeWant := "10.1/rc retracts 10.1/paper"

	if passiveRow.provenance != passiveWant {
		t.Fatalf("passive provenance = %q, want %q", passiveRow.provenance, passiveWant)
	}
	if passiveRow.paperDOI != "10.1/paper" {
		t.Fatalf("passive paper_doi = %q, want 10.1/paper", passiveRow.paperDOI)
	}
	if passiveRow.retractionDOI != "10.1/rc" {
		t.Fatalf("passive retraction_doi = %q, want 10.1/rc", passiveRow.retractionDOI)
	}

	if activeRow.provenance != activeWant {
		t.Fatalf("active provenance = %q, want %q", activeRow.provenance, activeWant)
	}
	if activeRow.paperDOI != "10.1/rc" {
		t.Fatalf("active paper_doi = %q, want 10.1/rc", activeRow.paperDOI)
	}
	if activeRow.retractionDOI != "10.1/paper" {
		t.Fatalf("active retraction_doi = %q, want 10.1/paper", activeRow.retractionDOI)
	}
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

func TestRetractionProvenance_BuildsRequestedString(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/yes"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes"}})

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
	if got := countRows(t, db, retractionProvenanceViewName); got != 2 {
		t.Fatalf("retraction_provenance rows = %d, want 2 (1 passive + 1 active)", got)
	}

	var provenanceTypes []string
	rows, err := db.Query(`SELECT provenance FROM ` + retractionProvenanceViewName + ` ORDER BY provenance`)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan view row: %v", err)
		}
		provenanceTypes = append(provenanceTypes, p)
	}
	rows.Close()

	wantProvenances := map[string]bool{
		"10.1/yes is-retracted-by 10.1/no": true,
		"10.1/no retracts 10.1/yes":         true,
	}
	for _, p := range provenanceTypes {
		if !wantProvenances[p] {
			t.Fatalf("unexpected provenance %q, want one of %v", p, wantProvenances)
		}
	}
	if len(provenanceTypes) != len(wantProvenances) {
		t.Fatalf("provenance count = %d, want %d (1 passive + 1 active)", len(provenanceTypes), len(wantProvenances))
	}
}

func TestRetractionProvenance_NoMatchYieldsEmptyView(t *testing.T) {
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
	if !viewExists(t, db, retractionProvenanceViewName) {
		t.Fatalf("retraction_provenance view missing though both tables exist")
	}
	if got := countRows(t, db, retractionProvenanceViewName); got != 0 {
		t.Fatalf("retraction_provenance rows = %d, want 0 (no matching DOI)", got)
	}
}

func TestRetractionProvenance_MultipleNotices(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/yes"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc1"}`,
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc2"}`,
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
	if got := countRows(t, db, retractionProvenanceViewName); got != 4 {
		t.Fatalf("retraction_provenance rows = %d, want 4 (1 per notice x 2 directions)", got)
	}

	provenances := map[string]bool{}
	rows, err := db.Query(`SELECT provenance FROM ` + retractionProvenanceViewName)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan view row: %v", err)
		}
		provenances[p] = true
	}
	rows.Close()
	if !provenances["10.1/yes is-retracted-by 10.1/rc1"] {
		t.Fatalf("missing passive for rc1, got %v", provenances)
	}
	if !provenances["10.1/yes is-retracted-by 10.1/rc2"] {
		t.Fatalf("missing passive for rc2, got %v", provenances)
	}
	if !provenances["10.1/rc1 retracts 10.1/yes"] {
		t.Fatalf("missing active for rc1, got %v", provenances)
	}
	if !provenances["10.1/rc2 retracts 10.1/yes"] {
		t.Fatalf("missing active for rc2, got %v", provenances)
	}
}

func TestRetractionProvenance_IdempotentAcrossReRuns(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/yes"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc1"}`,
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc2"}`,
		},
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
	if !viewExists(t, db, retractionProvenanceViewName) {
		t.Fatalf("retraction_provenance view missing after re-runs")
	}
	if got := countRows(t, db, retractionProvenanceViewName); got != 4 {
		t.Fatalf("retraction_provenance rows after re-runs = %d, want 4 (2 pairs x 2 directions)", got)
	}

	want := map[string]bool{
		"10.1/yes is-retracted-by 10.1/rc1": true,
		"10.1/yes is-retracted-by 10.1/rc2": true,
		"10.1/rc1 retracts 10.1/yes":        true,
		"10.1/rc2 retracts 10.1/yes":        true,
	}
	rows, err := db.Query(`SELECT provenance FROM ` + retractionProvenanceViewName)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan view row: %v", err)
		}
		got[p] = true
	}
	rows.Close()
	if len(got) != len(want) {
		t.Fatalf("provenance rows differ after re-runs: got %v, want %v", got, want)
	}
	for p := range want {
		if !got[p] {
			t.Fatalf("missing %q after re-runs, got %v", p, got)
		}
	}
}

func TestRetractionProvenance_AppearsOnlyWhenBothTablesExist(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
 writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/matched"}`})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/matched"}})
	outDB := filepath.Join(t.TempDir(), "vulcanus.duckdb")
	if _, err := Run(ctx, nStore, visDir, outDB); err != nil {
		t.Fatalf("nauvis Run: %v", err)
	}

	if viewExists(t, freshDB(t, outDB), retractionProvenanceViewName) {
		t.Fatalf("retraction_provenance view present after nauvis-only ingest; want absent")
	}

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {`{"OriginalPaperDOI":"10.1/matched","RetractionDOI":"10.1/matched"}`},
	})
	if _, err := Run(ctx, fStore, fDir, outDB); err != nil {
		t.Fatalf("fulgora Run: %v", err)
	}
	if !viewExists(t, freshDB(t, outDB), retractionProvenanceViewName) {
		t.Fatalf("retraction_provenance view missing after both ingests")
	}
}

func TestRetractionProvenance_MatchesRetractionsView(t *testing.T) {
	ctx := context.Background()

	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{
		`{"DOI":"10.1/yes","title":"N matched"}`,
		`{"DOI":"10.1/nope","title":"N unmatched"}`,
	})
	nStore := openStore(t, map[string][]string{"0.ndjson": {"10.1/yes", "10.1/nope"}})

	fStore, fDir := openFulgoraStoreWithLines(t, map[string][]string{
		"retractionwatch": {
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc1"}`,
			`{"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/rc2"}`,
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

	var rawPairs []string
	rows, err := db.Query(`SELECT paper_doi, retraction_doi FROM ` + retractionProvenanceViewName + ` ORDER BY 1, 2`)
	if err != nil {
		t.Fatalf("query provenance view: %v", err)
	}
	for rows.Next() {
		var paper, retraction string
		if err := rows.Scan(&paper, &retraction); err != nil {
			t.Fatalf("scan provenance row: %v", err)
		}
		rawPairs = append(rawPairs, paper+"|"+retraction)
	}
	rows.Close()

	// Each Nauvis↔retractionwatch match yields two pairs:
	// (DOI | RetractionDOI) from the passive row and
	// (RetractionDOI | DOI) from the active row. Both must exist in retractions.
	pairSeen := map[string]bool{}
	for _, p := range rawPairs {
		pairSeen[p] = true
	}

	rws, err := db.Query(`SELECT matched_doi, json_extract_string(retractionwatch_record, '$.RetractionDOI') AS retraction_doi ` +
		`FROM ` + retractionsViewName)
	if err != nil {
		t.Fatalf("query retractions view: %v", err)
	}
	retPairs := map[string]bool{}
	for rws.Next() {
		var paper, retraction string
		if err := rws.Scan(&paper, &retraction); err != nil {
			t.Fatalf("scan retractions row: %v", err)
		}
		retPairs[paper+"|"+retraction] = true
		retPairs[retraction+"|"+paper] = true
	}
	rws.Close()

	// Every raw pair from provenance must appear in retractions (either direction).
	for p := range pairSeen {
		if !retPairs[p] {
			t.Fatalf("provenance pair %q not found in retractions view (or reverse); views disagree", p)
		}
	}

	// Every retractions pair must appear somewhere in provenance (either direction).
	for rp := range retPairs {
		if !pairSeen[rp] {
			t.Fatalf("retractions pair %q not found in provenance view; views disagree", rp)
		}
	}
}
