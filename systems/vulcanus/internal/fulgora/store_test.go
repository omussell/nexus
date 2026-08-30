package fulgora

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const root = "systems/fulgora"

// openStore creates a fulgora SQLite database seeded with the passed sources.
// Each source is seeded with a single version, so no source ever has more than
// one "latest" row. Stored output paths are relative to root.
func openStore(t *testing.T, rows map[string]string) *Store {
	t.Helper()
	store, err := Open(t.TempDir()+"/fulgora.sqlite3", root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.Exec(
		`CREATE TABLE datasets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL, version TEXT NOT NULL,
			collected_at TEXT NOT NULL, initial_input TEXT NOT NULL,
			output TEXT NOT NULL,
			UNIQUE (source, version))`,
	); err != nil {
		t.Fatalf("create datasets: %v", err)
	}
	if _, err := store.db.Exec(
		`CREATE TABLE latest (
			source TEXT PRIMARY KEY, version TEXT NOT NULL,
			dataset_id INTEGER NOT NULL REFERENCES datasets (id))`,
	); err != nil {
		t.Fatalf("create latest: %v", err)
	}

	for name, v := range rows {
		out := filepath.Join(name, "output", name+"-"+v+".json")
		in := filepath.Join(name, "initial_input", name+"-in.csv")
		res, err := store.db.Exec(
			`INSERT INTO datasets (source, version, collected_at, initial_input, output)
			 VALUES (?, ?, '2026-01-01T00:00:00Z', ?, ?)`,
			name, v, in, out,
		)
		if err != nil {
			t.Fatalf("insert dataset %s/%s: %v", name, v, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("last id: %v", err)
		}
		if _, err := store.db.Exec(
			`INSERT INTO latest (source, version, dataset_id) VALUES (?, ?, ?)`,
			name, v, id,
		); err != nil {
			t.Fatalf("insert latest %s: %v", name, err)
		}
	}
	return store
}

func TestSources_SortsBySourceName(t *testing.T) {
	store := openStore(t, map[string]string{
		"ror":             "1.34",
		"retractionwatch": "2026-08-26",
	})
	sources, err := store.Sources(context.Background())
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("Sources len = %d, want 2: %+v", len(sources), sources)
	}
	if sources[0].Name() != "retractionwatch" || sources[1].Name() != "ror" {
		t.Fatalf("Sources order wrong: %+v", sources)
	}
}

func TestSourcesResolvesRoot(t *testing.T) {
	store := openStore(t, map[string]string{"ror": "1.34"})
	sources, err := store.Sources(context.Background())
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	got := sources[0].OutputPath()
	want := filepath.Join(root, "ror", "output", "ror-1.34.json")
	if got != want {
		t.Fatalf("OutputPath = %q, want %q", got, want)
	}
}

func TestVersion(t *testing.T) {
	store := openStore(t, map[string]string{"ror": "1.34"})
	v, err := store.Version(context.Background(), "ror")
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "1.34" {
		t.Fatalf("Version = %q, want 1.34", v)
	}
	if _, err := store.Version(context.Background(), "missing"); err != sql.ErrNoRows {
		t.Fatalf("Version err = %v, want sql.ErrNoRows", err)
	}
}

func TestVersionEmptyRoot(t *testing.T) {
	if _, err := Open(t.TempDir()+"/fulgora.sqlite3", ""); err == nil {
		t.Fatalf("expected error for empty root")
	}
}
