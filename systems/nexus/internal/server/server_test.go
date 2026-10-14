package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexus/nexus/internal/db"
	"github.com/nexus/nexus/internal/migrate"
	"github.com/nexus/nexus/internal/server"
	"github.com/nexus/nexus/internal/store"
	_ "modernc.org/sqlite"
)

func openNexusStore(t *testing.T, path string) *store.Store {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+path+"?_fk=1&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s, err := store.New(conn, db.New(conn))
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	return s
}

type relEntry struct {
	Croid        string `json:"croid"`
	Relationship string `json:"relationship"`
	RelatedCroid string `json:"related_croid"`
	CreatedAt    string `json:"created_at"`
}

func TestHandleCroid_404(t *testing.T) {
	tmpDir := t.TempDir()
	st := openNexusStore(t, tmpDir+"/nexus.db")

	srv := server.New(st, nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/croid/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	if !json.Valid(rec.Body.Bytes()) {
		t.Fatal("expected valid JSON response")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body["error"] != "croid not found" {
		t.Fatalf("expected 'croid not found', got %v", body["error"])
	}
}

func TestHandleCroid_WithData(t *testing.T) {
	tmpDir := t.TempDir()
	st := openNexusStore(t, tmpDir+"/nexus.db")

	ctx := context.Background()

	// Insert a couple of relationships.
	croidA := "testcroidaaaaaaaaaaaaaaaaaaaaaaaaaa"
	croidB := "testcroidbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	croidC := "testcroidcccccccccccccccccccccccccccc"

	_, _, err := st.Insert(ctx, store.Relationship{
		Source:       croidA,
		Target:       croidB,
		Relationship: "cites",
		Evidence:     "from vulcanus",
	})
	if err != nil {
		t.Fatalf("insert cites: %v", err)
	}

	_, _, err = st.Insert(ctx, store.Relationship{
		Source:       croidB,
		Target:       croidA,
		Relationship: "cited-by",
	})
	if err != nil {
		t.Fatalf("insert cited-by: %v", err)
	}

	_, _, err = st.Insert(ctx, store.Relationship{
		Source:       croidA,
		Target:       croidC,
		Relationship: "supplements",
	})
	if err != nil {
		t.Fatalf("insert supplements: %v", err)
	}

	srv := server.New(st, nil)
	handler := srv.Handler()

	// GET /croid/<croidA>
	req := httptest.NewRequest(http.MethodGet, "/croid/"+croidA, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []relEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// All entries should reference the queried CROID.
	for i, e := range entries {
		if e.Croid != croidA {
			t.Fatalf("entry %d: croid = %q, want %q", i, e.Croid, croidA)
		}
	}

	// Verify the two outgoing and one incoming relationships are present.
	rels := make(map[string]string) // relationship -> other croid
	for _, e := range entries {
		rels[e.Relationship] = e.RelatedCroid
	}
	if rels["cites"] != croidB {
		t.Fatalf("expected cites -> %q, got %q", croidB, rels["cites"])
	}
	if rels["supplements"] != croidC {
		t.Fatalf("expected supplements -> %q, got %q", croidC, rels["supplements"])
	}
	if rels["cited-by"] != croidB {
		t.Fatalf("expected cited-by from %q, got %q", croidB, rels["cited-by"])
	}
}

func TestHandleRoot(t *testing.T) {
	tmpDir := t.TempDir()
	st := openNexusStore(t, tmpDir+"/nexus.db")

	srv := server.New(st, nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body["service"] != "nexus" {
		t.Fatalf("expected service nexus, got %q", body["service"])
	}
}

func TestHandleHealth(t *testing.T) {
	tmpDir := t.TempDir()
	st := openNexusStore(t, tmpDir+"/nexus.db")

	srv := server.New(st, nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}
