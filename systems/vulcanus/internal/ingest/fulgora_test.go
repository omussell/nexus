package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/nexus/vulcanus/internal/fulgora"
)

// openFulgoraStore seeds a real fulgora SQLite registry with one latest dataset
// per source and writes the matching NDJSON output files under dir. The Store
// is opened with dir as its data root so OutputPath() resolves against the
// seeded files. Each test manages its own outDB path.
func openFulgoraStore(t *testing.T, sources []string, dir string) *fulgora.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "fulgora.sqlite3")

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(
		`CREATE TABLE datasets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL, version TEXT NOT NULL,
			collected_at TEXT NOT NULL, initial_input TEXT NOT NULL,
			output TEXT NOT NULL,
			UNIQUE (source, version))`,
	); err != nil {
		t.Fatalf("create datasets: %v", err)
	}
	if _, err := conn.Exec(
		`CREATE TABLE latest (
			source TEXT PRIMARY KEY, version TEXT NOT NULL,
			dataset_id INTEGER NOT NULL REFERENCES datasets (id))`,
	); err != nil {
		t.Fatalf("create latest: %v", err)
	}

	for _, name := range sources {
		v := "1.0"
		outRel := filepath.Join(name, "output", name+"-"+v+".json")
		inRel := filepath.Join(name, "initial_input", name+"-in.csv")

		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(outRel)), 0o755); err != nil {
			t.Fatalf("mkdir output dir for %s: %v", name, err)
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(inRel)), 0o755); err != nil {
			t.Fatalf("mkdir input dir for %s: %v", name, err)
		}

		line := fmt.Sprintf(`{"id":1,"name":"%s"}`, name)
		if err := os.WriteFile(filepath.Join(dir, outRel), []byte(line+"\n"), 0o644); err != nil {
			t.Fatalf("write output for %s: %v", name, err)
		}

		res, err := conn.Exec(
			`INSERT INTO datasets (source, version, collected_at, initial_input, output)
			 VALUES (?, ?, '2026-01-01T00:00:00Z', ?, ?)`,
			name, v, inRel, outRel,
		)
		if err != nil {
			t.Fatalf("insert dataset %s: %v", name, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last id: %v", err)
		}
		if _, err := conn.Exec(
			`INSERT INTO latest (source, version, dataset_id) VALUES (?, ?, ?)`,
			name, v, id,
		); err != nil {
			t.Fatalf("insert latest %s: %v", name, err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	store, err := fulgora.Open(dbPath, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// freshDB opens a new DuckDB connection on outDB for reading after Run fills it.
func freshDB(t *testing.T, outDB string) *sql.DB {
	t.Helper()
	conn, err := duckdb.NewConnector(outDB, nil)
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return sql.OpenDB(conn)
}

func TestFulgoraPerSourceTables(t *testing.T) {
	ctx := context.Background()
	sources := []string{"ror", "retractionwatch"}
	dir := t.TempDir()

	store := openFulgoraStore(t, sources, dir)

	outDB := filepath.Join(t.TempDir(), "fulgora.duckdb")
	n, err := Run(ctx, store, dir, outDB)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Fatalf("rows ingested = %d, want 2", n)
	}

	db := freshDB(t, outDB)
	for _, name := range sources {
		if got := countRows(t, db, name); got != 1 {
			t.Fatalf("table %s rows = %d, want 1", name, got)
		}
	}
	got := tableNames(t, db)
	wantSet := map[string]bool{}
	for _, name := range sources {
		wantSet[name] = true
	}
	if len(got) != len(wantSet) {
		t.Fatalf("tables = %v, want exactly %v", got, sources)
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Fatalf("tables %v include unexpected %q", got, g)
		}
	}
}

func TestFulgoraIdempotentReRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store := openFulgoraStore(t, []string{"ror"}, dir)
	outDB := filepath.Join(t.TempDir(), "fulgora.duckdb")

	if _, err := Run(ctx, store, dir, outDB); err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	db := freshDB(t, outDB)
	if got := countRows(t, db, "ror"); got != 1 {
		t.Fatalf("after run 1: got %d, want 1", got)
	}

	if _, err := Run(ctx, store, dir, outDB); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	db = freshDB(t, outDB)
	if got := countRows(t, db, "ror"); got != 1 {
		t.Fatalf("after run 2: got %d, want 1", got)
	}
}

func TestFulgoraInvalidJSONFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store := openFulgoraStore(t, []string{"ror"}, dir)
	outDB := filepath.Join(t.TempDir(), "fulgora.duckdb")

	outFile := filepath.Join(dir, "ror", "output", "ror-1.0.json")
	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content = append(content, []byte("not-json")...)
	if err := os.WriteFile(outFile, content, 0o644); err != nil {
		t.Fatalf("corrupt output: %v", err)
	}

	if _, err := Run(ctx, store, dir, outDB); err == nil {
		t.Fatal("expected failure on invalid JSON, got nil")
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	row := db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s`, quoted(table)))
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	// Base tables only — the `retractions` view (a derived object) is not
	// treated as a table the way a provider's own table is.
	rows, err := db.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'main' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatalf("query table names: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	return tables
}
