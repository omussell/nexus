package croidclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMint_Success(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatal("expected POST")
		}
		var body MintRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.CroType != "DOI" || body.CroValue != "10.1/test" || body.System != "nauvis" {
			t.Fatalf("unexpected body: %+v", body)
		}
		resp := MintResponse{
			Croid:     "aaaaa12345_aaaaa12345-aaaaa12345",
			CroType:   "DOI",
			CroValue:  "10.1/test",
			System:    "nauvis",
			CreatedAt: "2024-01-01T00:00:00Z",
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})

	client := New(server.URL)
	croid, err := client.Mint(context.Background(), "10.1/test")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if croid != "aaaaa12345_aaaaa12345-aaaaa12345" {
		t.Errorf("croid = %q, want %q", croid, "aaaaa12345_aaaaa12345-aaaaa12345")
	}
}

func TestMint_Duplicate(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		resp := MintResponse{
			Croid:     "existing_croid_existing_croid-existing_croid",
			CroType:   "DOI",
			CroValue:  "10.1/dup",
			System:    "nauvis",
			CreatedAt: "2024-01-01T00:00:00Z",
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	})

	client := New(server.URL)
	croid, err := client.Mint(context.Background(), "10.1/dup")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if croid != "existing_croid_existing_croid-existing_croid" {
		t.Errorf("croid = %q, want %q", croid, "existing_croid_existing_croid-existing_croid")
	}
}

func TestMint_Error(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid"}`))
	})

	client := New(server.URL)
	_, err := client.Mint(context.Background(), "10.1/bad")
	if err == nil {
		t.Fatal("expected error")
	}
}
