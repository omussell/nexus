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

	if _, err := store.db.Exec(`CREATE TABLE items (file TEXT, doi TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for file, dois := range rows {
		for _, doi := range dois {
			if _, err := store.db.Exec(`INSERT INTO items (file, doi) VALUES (?, ?)`, file, doi); err != nil {
				t.Fatalf("insert row: %v", err)
			}
		}
	}
	return store
}

func TestItems_Basic(t *testing.T) {
	store := openStore(t, map[string][]string{
		"a.ndjson": {"10.1/a"},
		"b.ndjson": {"10.1/b", "10.1/c"},
	})

	items, err := store.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	want := []Item{{Path: "a.ndjson", Count: 1}, {Path: "b.ndjson", Count: 2}}
	if len(items) != len(want) {
		t.Fatalf("Items len = %d, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		if items[i] != w {
			t.Fatalf("Items[%d] = %+v, want %+v", i, items[i], w)
		}
	}
}

func TestItems_SortsByPath(t *testing.T) {
	store := openStore(t, map[string][]string{
		"z.ndjson": {"10.1/z"},
		"a.ndjson": {"10.1/a"},
	})
	items, err := store.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if items[0].Path != "a.ndjson" {
		t.Fatalf("first item = %q, want a.ndjson", items[0].Path)
	}
}

func TestItems_NoRows(t *testing.T) {
	store := openStore(t, map[string][]string{})
	items, err := store.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if items != nil {
		t.Fatalf("Items = %+v, want nil", items)
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
