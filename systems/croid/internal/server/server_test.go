package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newTestServer builds a Server backed by a temp SQLite file.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite3")
	srv, err := New(context.Background(), dbPath, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func post(t *testing.T, srv *Server, body map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/croid", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	return w, got
}

func TestCreateAndFetch(t *testing.T) {
	srv := newTestServer(t)

	w, body := post(t, srv, map[string]string{
		"cro_type":  "DOI",
		"cro_value": "10.5555/12345678",
		"system":    "nauvis",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	croid, _ := body["croid"].(string)
	_ = croid
	if len(croid) != 32 {
		t.Fatalf("croid = %q (%d chars), want a 32-char string", croid, len(croid))
	}
	if croid == "" {
		t.Fatal("croid is empty")
	}

	// GET by CROID.
	req := httptest.NewRequest(http.MethodGet, "/croid/"+croid, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["cro_value"] != "10.5555/12345678" {
		t.Errorf("GET cro_value = %v, want \"10.5555/12345678\"", got["cro_value"])
	}
	if got["croid"] != croid {
		t.Errorf("GET croid = %v, want %q", got["croid"], croid)
	}
	if _, ok := got["created_at"].(string); !ok {
		t.Errorf("GET created_at missing or not a string: %v", got["created_at"])
	}
}

func TestCreateIDempotent(t *testing.T) {
	srv := newTestServer(t)
	id := map[string]string{
		"cro_type":  "ORCID",
		"cro_value": "0000-0001-2345-6789",
		"system":    "fulgora",
	}
	_, first := post(t, srv, id)
	_, second := post(t, srv, id)
	if first["croid"] != second["croid"] {
		t.Fatalf("dedupe failed: %q vs %q", first["croid"], second["croid"])
	}
	if first["created_at"] != second["created_at"] {
		t.Fatalf("created_at drifted on dedupe: %q vs %q", first["created_at"], second["created_at"])
	}
}

func TestGetUnknown(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/croid/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateMissingFields(t *testing.T) {
	srv := newTestServer(t)
	cases := []map[string]string{
		{},
		{"cro_type": "DOI", "cro_value": "10.5555/xx", "system": ""},
		{"cro_type": "DOI", "cro_value": "", "system": "nauvis"},
		{"cro_type": "", "cro_value": "10.5555/xx", "system": "nauvis"},
	}
	for i, c := range cases {
		raw, _ := json.Marshal(c)
		req := httptest.NewRequest(http.MethodPost, "/croid", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400; body=%s", i, rec.Code, rec.Body.String())
		}
	}
}

func TestMalformedJSON(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/croid", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestMigrateIsIdempotent confirms opening a second Server on the same file
// works (schema already applied).
func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite3")
	if _, err := New(context.Background(), dbPath, nil); err != nil {
		t.Fatalf("first New: %v", err)
	}
	srv2, err := New(context.Background(), dbPath, nil)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	srv2.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file missing: %v", err)
	}
}
