package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			"croid":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"cro_type":  "DOI",
			"cro_value": "10.0.0/foo",
			"system":    "nauvis",
			"created_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	c := client.New(server.URL)
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

	c := client.New(server.URL)
	_, err := c.Mint(context.Background(), "10.0.0/bar")
	if err == nil {
		t.Fatal("expected error")
	}
}
