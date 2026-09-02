package nauvis

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openStore(t *testing.T, rows map[string][]string) *Store {
	t.Helper()
	store, err := Open(t.TempDir() + "/nauvis.sqlite3")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.Exec(`CREATE TABLE nauvis (file TEXT, doi TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for file, dois := range rows {
		for _, doi := range dois {
			if _, err := store.db.Exec(`INSERT INTO nauvis (file, doi) VALUES (?, ?)`, file, doi); err != nil {
				t.Fatalf("insert row: %v", err)
			}
		}
	}
	return store
}

func TestNauvis_Basic(t *testing.T) {
	store := openStore(t, map[string][]string{
		"a.ndjson": {"10.1/a"},
		"b.ndjson": {"10.1/b", "10.1/c"},
	})

	navis, err := store.Nauvis()
	if err != nil {
		t.Fatalf("Nauvis: %v", err)
	}
	want := []Nauvis{{Path: "a.ndjson", Count: 1}, {Path: "b.ndjson", Count: 2}}
	if len(navis) != len(want) {
		t.Fatalf("Nauvis len = %d, want %d: %+v", len(navis), len(want), navis)
	}
	for i, w := range want {
		if navis[i] != w {
			t.Fatalf("Nauvis[%d] = %+v, want %+v", i, navis[i], w)
		}
	}
}

func TestNauvis_SortsByPath(t *testing.T) {
	store := openStore(t, map[string][]string{
		"z.ndjson": {"10.1/z"},
		"a.ndjson": {"10.1/a"},
	})
	navis, err := store.Nauvis()
	if err != nil {
		t.Fatalf("Nauvis: %v", err)
	}
	if navis[0].Path != "a.ndjson" {
		t.Fatalf("first nauvis = %q, want a.ndjson", navis[0].Path)
	}
}

func TestNauvis_NoRows(t *testing.T) {
	store := openStore(t, map[string][]string{})
	navis, err := store.Nauvis()
	if err != nil {
		t.Fatalf("Nauvis: %v", err)
	}
	if navis != nil {
		t.Fatalf("Nauvis = %+v, want nil", navis)
	}
}

func TestByDOI_Found(t *testing.T) {
	store := openStore(t, map[string][]string{
		"a.ndjson": {"10.1/a"},
	})
	file, err := store.ByDOI(context.Background(), "10.1/a")
	if err != nil {
		t.Fatalf("ByDOI: %v", err)
	}
	if file != "a.ndjson" {
		t.Fatalf("ByDOI = %q, want a.ndjson", file)
	}
}

func TestByDOI_NotFound(t *testing.T) {
	store := openStore(t, map[string][]string{
		"a.ndjson": {"10.1/a"},
	})
	_, err := store.ByDOI(context.Background(), "10.1/nope")
	if err != sql.ErrNoRows {
		t.Fatalf("ByDOI err = %v, want sql.ErrNoRows", err)
	}
}
