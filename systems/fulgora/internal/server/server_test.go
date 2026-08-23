package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nexus/fulgora/internal/store"
)

func newServer(t *testing.T, seed ...store.Dataset) *Server {
	t.Helper()
	ctx := context.Background()
	s, err := New(ctx, filepath.Join(t.TempDir(), "srv.sqlite3"), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	for i := range seed {
		if seed[i].CollectedAt.IsZero() {
			seed[i].CollectedAt = now
		}
		if _, err := s.store.Record(ctx, seed[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	return s
}

func get(t *testing.T, hs http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	hs.ServeHTTP(rec, req)
	var body map[string]any
	if len(rec.Body.Bytes()) > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec.Code, body
}

func getList(t *testing.T, hs http.Handler, path string) (int, []map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	hs.ServeHTTP(rec, req)
	var body []map[string]any
	if len(rec.Body.Bytes()) > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec.Code, body
}

func TestHandlerHealthz(t *testing.T) {
	s := newServer(t)
	code, body := get(t, s.Handler(), "/healthz")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz = %d %v", code, body)
	}
}

func TestHandlerLatestSingle(t *testing.T) {
	s := newServer(t, store.Dataset{Source: "ror", Version: "1.63"})
	code, body := get(t, s.Handler(), "/latest/ror")
	if code != http.StatusOK {
		t.Fatalf("status = %d body=%v", code, body)
	}
	if body["version"] != "1.63" {
		t.Fatalf("version = %v", body["version"])
	}
}

func TestHandlerLatestMissing404(t *testing.T) {
	s := newServer(t)
	code, _ := get(t, s.Handler(), "/latest/does-not-exist")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestHandlerLatestAll(t *testing.T) {
	s := newServer(t,
		store.Dataset{Source: "ror", Version: "1.63"},
		store.Dataset{Source: "retractionwatch", Version: "2026-08-15"},
	)
	code, list := getList(t, s.Handler(), "/latest")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}
}

func TestHandlerHistory(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, v := range []string{"1.63", "1.64", "1.65"} {
		if _, err := s.store.Record(ctx, store.Dataset{
			Source: "ror", Version: v, CollectedAt: now,
			InitialInput: "in/" + v, Output: "out/" + v,
		}); err != nil {
			t.Fatalf("record %s: %v", v, err)
		}
	}
	code, list := getList(t, s.Handler(), "/latest/ror/history")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(list) != 3 {
		t.Fatalf("history len = %d, want 3", len(list))
	}
	// Ordered by collected_at DESC; all three share the same timestamp in the
	// seed, so we only assert the set of versions is present.
	seen := map[string]bool{}
	for _, m := range list {
		seen[m["version"].(string)] = true
	}
	for _, v := range []string{"1.63", "1.64", "1.65"} {
		if !seen[v] {
			t.Fatalf("history missing %s: %v", v, list)
		}
	}
}

func TestHandlerRootBanner(t *testing.T) {
	s := newServer(t)
	code, body := get(t, s.Handler(), "/")
	if code != http.StatusOK || body["service"] != "fulgora" {
		t.Fatalf("root = %d %v", code, body)
	}
}
