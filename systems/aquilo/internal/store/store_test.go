package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nexus/aquilo/internal/db"
	"github.com/nexus/aquilo/internal/migrate"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "store.db")+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(context.Background(), "PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(conn, db.New(conn))
}

func TestInsertItem(t *testing.T) {
	s := openStore(t)
	croid := "test_croid_test_croid-test_croid001"
	rec := `{"DOI":"10.5555/test","title":"Test Article"}`
	if err := s.InsertItem(context.Background(), croid, rec); err != nil {
		t.Fatalf("InsertItem: %v", err)
	}

	count, err := s.CountItems(context.Background())
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountItems = %d, want 1", count)
	}
}

func TestInsertItemsBulk(t *testing.T) {
	s := openStore(t)
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
		"test_croid_test_croid-test_croid003",
	}
	records := []string{
		`{"DOI":"10.5555/a","title":"A"}`,
		`{"DOI":"10.5555/b","title":"B"}`,
		`{"DOI":"10.5555/c","title":"C"}`,
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	count, err := s.CountItems(context.Background())
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountItems = %d, want 3", count)
	}
}

func TestInsertItemsBulkEmpty(t *testing.T) {
	s := openStore(t)
	if err := s.InsertItemsBulk(context.Background(), []string{}, []string{}); err != nil {
		t.Fatalf("InsertItemsBulk([]): %v", err)
	}
	count, err := s.CountItems(context.Background())
	if err != nil {
		t.Fatalf("CountItems: %v", err)
	}
	if count != 0 {
		t.Fatalf("CountItems = %d, want 0", count)
	}
}

func TestGetItemByCroid(t *testing.T) {
	s := openStore(t)
	croid := "test_croid_test_croid-test_croid001"
	rec := `{"DOI":"10.5555/get","title":"Get Test"}`
	if err := s.InsertItem(context.Background(), croid, rec); err != nil {
		t.Fatalf("InsertItem: %v", err)
	}

	item, err := s.GetItemByCroid(context.Background(), croid)
	if err != nil {
		t.Fatalf("GetItemByCroid: %v", err)
	}
	if item.Croid != croid {
		t.Fatalf("Croid = %q, want %q", item.Croid, croid)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(item.RecordJson), &got); err != nil {
		t.Fatalf("Unmarshal RecordJson: %v", err)
	}
	if got["DOI"] != "10.5555/get" {
		t.Errorf("DOI = %v, want 10.5555/get", got["DOI"])
	}
}

func TestGetItemByCroidNotFound(t *testing.T) {
	s := openStore(t)
	_, err := s.GetItemByCroid(context.Background(), "nonexistent_croid")
	if err != sql.ErrNoRows {
		t.Fatalf("GetItemByCroid(nonexistent) = %v, want sql.ErrNoRows", err)
	}
}

func TestQueryItems(t *testing.T) {
	s := openStore(t)
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
		"test_croid_test_croid-test_croid003",
	}
	records := []string{
		`{"DOI":"10.5555/1","title":"One"}`,
		`{"DOI":"10.5555/2","title":"Two"}`,
		`{"DOI":"10.5555/3","title":"Three"}`,
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	items, err := s.QueryItems(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("QueryItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("QueryItems returned %d items, want 2", len(items))
	}
	if items[0].Croid == "" || items[1].Croid == "" {
		t.Fatalf("Croids are empty")
	}
}

func TestQueryItemsPaginated(t *testing.T) {
	s := openStore(t)
	for i := 0; i < 5; i++ {
		croid := "test_croid_test_croid-test_croid" + string(rune('0'+i))
		rec := `{"DOI":"10.5555/p","title":"P"}`
		if err := s.InsertItem(context.Background(), croid, rec); err != nil {
			t.Fatalf("InsertItem: %v", err)
		}
	}

	items, err := s.QueryItems(context.Background(), 2, 2)
	if err != nil {
		t.Fatalf("QueryItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("QueryItems returned %d items, want 2", len(items))
	}
	if items[0].Croid == "" {
		t.Fatalf("first Croid is empty")
	}
}

func TestListItemCroids(t *testing.T) {
	s := openStore(t)
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
	}
	records := []string{
		`{"DOI":"10.5555/id1","title":"ID1"}`,
		`{"DOI":"10.5555/id2","title":"ID2"}`,
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	crids, err := s.ListItemCroids(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("ListItemCroids: %v", err)
	}
	if len(crids) != 2 {
		t.Fatalf("ListItemCroids returned %d croids, want 2", len(crids))
	}
	if crids[0] == "" || crids[1] == "" {
		t.Fatalf("Croids are empty: %v", crids)
	}
}

func TestOpenEmptyPath(t *testing.T) {
	_, _, err := Open(context.Background(), "")
	if err == nil {
		t.Fatal("Open(\"\") should return an error")
	}
}
