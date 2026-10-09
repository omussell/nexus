package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nexus/croid/client"
)

func TestMint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/croid" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		var req client.MintRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.CroType != "DOI" || req.CroValue != "10.0.0/foo" || req.System != "nauvis" {
			t.Fatalf("unexpected request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"croid":      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"cro_type":   "DOI",
			"cro_value":  "10.0.0/foo",
			"system":     "nauvis",
			"created_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	c := client.New(server.URL, "nauvis")
	croid, err := c.Mint(context.Background(), "10.0.0/foo")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if croid != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("expected CROID, got %q", croid)
	}
}

func TestMintBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "invalid body",
		})
	}))
	defer server.Close()

	c := client.New(server.URL, "nauvis")
	_, err := c.Mint(context.Background(), "10.0.0/bar")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMintWith(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.MintRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.CroType != "dataset" || req.CroValue != "ror:1.34" || req.System != "fulgora" {
			t.Fatalf("unexpected request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"croid":      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"cro_type":   "dataset",
			"cro_value":  "ror:1.34",
			"system":     "fulgora",
			"created_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	c := client.New(server.URL, "fulgora")
	croid, err := c.MintWith(context.Background(), "dataset", "ror:1.34")
	if err != nil {
		t.Fatalf("MintWith: %v", err)
	}
	if croid != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("expected CROID, got %q", croid)
	}
}

func TestMintWithNilSystem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.MintRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !strings.HasPrefix(req.CroValue, "10.0.0/") {
			t.Fatalf("unexpected cro_value: %q", req.CroValue)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"croid":      "cccccccccccccccccccccccccccccccc",
			"cro_type":   "DOI",
			"cro_value":  "10.0.0/abc",
			"system":     "test",
			"created_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	c := client.New(server.URL, "test")
	croid, err := c.Mint(context.Background(), "10.0.0/abc")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if croid != "cccccccccccccccccccccccccccccccc" {
		t.Fatalf("expected CROID, got %q", croid)
	}
}
