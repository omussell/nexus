package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/nexus/vulcanus/internal/nauvis"
)

func openStore(t *testing.T, rows map[string][]string) *nauvis.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nauvis.sqlite3")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`CREATE TABLE nauvis (file TEXT, doi TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for file, dois := range rows {
		for _, doi := range dois {
			if _, err := conn.Exec(`INSERT INTO nauvis (file, doi) VALUES (?, ?)`, file, doi); err != nil {
				t.Fatalf("insert row: %v", err)
			}
		}
	}
	store, err := nauvis.Open(path)
	if err != nil {
		t.Fatalf("open nauvis: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func writeNDJSON(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	var body []byte
	for _, l := range lines {
		body = append(body, l...)
		body = append(body, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readRecords(t *testing.T, duckDB string) [][]byte {
	t.Helper()
	conn, err := duckdb.NewConnector(duckDB, nil)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	db := sql.OpenDB(conn)

	rows, err := db.Query(`SELECT record FROM items ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var rec interface{}
		if err := rows.Scan(&rec); err != nil {
			t.Fatalf("scan: %v", err)
		}
		bytes, _ := json.Marshal(rec)
		out = append(out, bytes)
	}
	return out
}

func TestRun_Basic(t *testing.T) {
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/a"}`})
	writeNDJSON(t, visDir, "1.ndjson", []string{`{"DOI":"10.1/b"}`, `{"DOI":"10.1/c"}`})
	store := openStore(t, map[string][]string{
		"0.ndjson": {"10.1/a"},
		"1.ndjson": {"10.1/b", "10.1/c"},
	})

	duckDB := t.TempDir() + "/out.duckdb"
	total, err := Run(context.Background(), store, visDir, duckDB)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	recs := readRecords(t, duckDB)
	if len(recs) != 3 {
		t.Fatalf("stored rows = %d, want 3", len(recs))
	}
}

func TestRun_Empty(t *testing.T) {
	store := openStore(t, map[string][]string{})
	duckDB := t.TempDir() + "/out.duckdb"
	total, err := Run(context.Background(), store, t.TempDir(), duckDB)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
}

func TestRun_InvalidJSONFails(t *testing.T) {
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/a"}`, "not json"})
	store := openStore(t, map[string][]string{
		"0.ndjson": {"10.1/a"},
	})

	_, err := Run(context.Background(), store, visDir, t.TempDir()+"/out.duckdb")
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestRun_SkipsEmptyLines(t *testing.T) {
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{``, `{"DOI":"10.1/a"}`, ``})
	store := openStore(t, map[string][]string{
		"0.ndjson": {"10.1/a"},
	})

	total, err := Run(context.Background(), store, visDir, t.TempDir()+"/out.duckdb")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
}

func TestRun_StoresFullItemContents(t *testing.T) {
	visDir := t.TempDir()
	writeNDJSON(t, visDir, "0.ndjson", []string{`{"DOI":"10.1/a","title":"The Whole Thing"}`})
	store := openStore(t, map[string][]string{
		"0.ndjson": {"10.1/a"},
	})

	duckDB := t.TempDir() + "/out.duckdb"
	if _, err := Run(context.Background(), store, visDir, duckDB); err != nil {
		t.Fatalf("Run: %v", err)
	}
	recs := readRecords(t, duckDB)
	if len(recs) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(recs))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(recs[0], &m); err != nil {
		t.Fatalf("record is not valid JSON: %v", err)
	}
	if m["title"] != "The Whole Thing" {
		t.Fatalf("stored record = %v, want full contents", m)
	}
}

func TestRun_MissingFileFails(t *testing.T) {
	store := openStore(t, map[string][]string{
		"missing.ndjson": {"10.1/a"},
	})
	_, err := Run(context.Background(), store, t.TempDir(), t.TempDir()+"/out.duckdb")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}
