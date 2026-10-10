package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nexus/aquilo/internal/db"
	"github.com/nexus/aquilo/internal/migrate"
	"github.com/nexus/aquilo/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.sqlite3")+"?_busy_timeout=5000")
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
	s := store.New(conn, db.New(conn))
	return New("localhost:0", s)
}

func get(t *testing.T, srv *Server, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return rec, body
}

func TestStatus(t *testing.T) {
	srv := newTestServer(t)

	rec, body := get(t, srv, "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /status status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"", body["status"])
	}
	if body["items"].(float64) != 0 {
		t.Errorf("items = %v, want 0", body["items"])
	}
}

func TestItemsCountEmpty(t *testing.T) {
	srv := newTestServer(t)

	rec, body := get(t, srv, "/items/count")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items/count status = %d, want 200", rec.Code)
	}
	if body["count"].(float64) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestItemsListEmpty(t *testing.T) {
	srv := newTestServer(t)

	rec, body := get(t, srv, "/items")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items status = %d, want 200", rec.Code)
	}
	if body["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", body["total"])
	}
	if body["offset"].(float64) != 0 {
		t.Errorf("offset = %v, want 0", body["offset"])
	}
	if body["limit"].(float64) != 50 {
		t.Errorf("limit = %v, want 50", body["limit"])
	}
}

func TestItemsListWithItems(t *testing.T) {
	srv := newTestServer(t)

	// Insert items using the store directly
	s := srv.store
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
		"test_croid_test_croid-test_croid003",
	}
	records := []string{
		`{"DOI":"10.5555/a","title":"Article A"}`,
		`{"DOI":"10.5555/b","title":"Article B"}`,
		`{"DOI":"10.5555/c","title":"Article C"}`,
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	rec, body := get(t, srv, "/items")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", body["total"])
	}

	items, ok := body["items"].([]any)
	if !ok {
		t.Fatal("items is not an array")
	}
	if len(items) != 3 {
		t.Fatalf("items has %d elements, want 3", len(items))
	}

	first := items[0].(map[string]any)
	if first["croid"] == "" {
		t.Errorf("first item croid is empty")
	}
	record := first["record"].(string)
	if !strings.Contains(record, "10.5555/a") {
		t.Errorf("record missing DOI: %s", record)
	}
}

func TestItemsListLimit(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
		"test_croid_test_croid-test_croid003",
		"test_croid_test_croid-test_croid004",
		"test_croid_test_croid-test_croid005",
	}
	records := make([]string, 5)
	for i := 0; i < 5; i++ {
		records[i] = `{"DOI":"10.5555/lim","title":"Limit"}`
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	rec, body := get(t, srv, "/items?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items?limit=2 status = %d", rec.Code)
	}
	if body["limit"].(float64) != 2 {
		t.Errorf("limit = %v, want 2", body["limit"])
	}
	if body["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", body["total"])
	}
}

func TestItemsListLimitOverMax(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croids := make([]string, 10)
	records := make([]string, 10)
	for i := 0; i < 10; i++ {
		croids[i] = "test_croid_test_croid-test_croid00" + string(rune('0'+i))
		records[i] = `{"DOI":"10.5555/lim","title":"Limit"}`
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	rec, body := get(t, srv, "/items?limit=1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items?limit=1000 status = %d", rec.Code)
	}
	if body["limit"].(float64) != 50 {
		t.Errorf("limit = %v, want 50 (max)", body["limit"])
	}
}

func TestItemByCroid(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croid := "test_croid_test_croid-test_croid001"
	if err := s.InsertItem(context.Background(), croid, `{"DOI":"10.5555/123","title":"ByCroid"}`); err != nil {
		t.Fatalf("InsertItem: %v", err)
	}

	rec, body := get(t, srv, "/items/"+croid)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items/{croid} status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if body["croid"] != croid {
		t.Errorf("croid = %v, want %s", body["croid"], croid)
	}
	record, ok := body["record"].(string)
	if !ok {
		t.Fatalf("record is not a string: %T", body["record"])
	}
	if !strings.Contains(record, "10.5555/123") {
		t.Errorf("record missing DOI: %s", record)
	}
}

func TestItemByCroidNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/items/nonexistent_croid", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /items/nonexistent_croid status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestItemByCroidEmpty(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/items/", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	// "/items/" doesn't match "/items/{croid}" pattern, so returns 404
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /items/ status = %d, want 404", rec.Code)
	}
}

func TestItemByCroidJSON(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croid := "test_croid_test_croid-test_croid001"
	if err := s.InsertItem(context.Background(), croid, `{"DOI":"10.5555/json","title":"JSON Test"}`); err != nil {
		t.Fatalf("InsertItem: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/croid/"+croid, nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /croid/{croid} status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	want := `{"DOI":"10.5555/json","title":"JSON Test"}`
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestItemByCroidJSONNotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/croid/nonexistent_croid", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /croid/nonexistent status = %d, want 404", rec.Code)
	}
}

func TestItemsListPagination(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croids := []string{
		"test_croid_test_croid-test_croid001",
		"test_croid_test_croid-test_croid002",
		"test_croid_test_croid-test_croid003",
		"test_croid_test_croid-test_croid004",
		"test_croid_test_croid-test_croid005",
	}
	records := make([]string, 5)
	for i := 0; i < 5; i++ {
		records[i] = `{"DOI":"10.5555/page","title":"Page"}`
	}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	rec, body := get(t, srv, "/items?offset=2&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items?offset=2&limit=2 status = %d", rec.Code)
	}
	if body["offset"].(float64) != 2 {
		t.Errorf("offset = %v, want 2", body["offset"])
	}
	if body["limit"].(float64) != 2 {
		t.Errorf("limit = %v, want 2", body["limit"])
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items has %d elements, want 2", len(items))
	}
	// offset=2 means we skip first 2 croids and get the 3rd and 4th
	first := items[0].(map[string]any)
	if first["croid"] == "" {
		t.Errorf("first item croid is empty")
	}
}

func TestItemsListInvalidParams(t *testing.T) {
	srv := newTestServer(t)

	s := srv.store
	croids := []string{"test_croid_test_croid-test_croid001"}
	records := []string{`{"DOI":"10.5555/valid","title":"Valid"}`}
	if err := s.InsertItemsBulk(context.Background(), croids, records); err != nil {
		t.Fatalf("InsertItemsBulk: %v", err)
	}

	// Invalid offset (non-numeric) should default to 0
	rec, body := get(t, srv, "/items?offset=abc")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items?offset=abc status = %d, want 200", rec.Code)
	}
	if body["offset"].(float64) != 0 {
		t.Errorf("offset = %v, want 0 for invalid offset", body["offset"])
	}

	// Invalid limit (non-numeric) should default to 50
	rec, body = get(t, srv, "/items?limit=abc")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /items?limit=abc status = %d, want 200", rec.Code)
	}
	if body["limit"].(float64) != 50 {
		t.Errorf("limit = %v, want 50 for invalid limit", body["limit"])
	}
}
