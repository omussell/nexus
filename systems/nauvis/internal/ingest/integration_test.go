package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_WithParquet(t *testing.T) {
	// Mock CROID server
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"croid":"parquet_croid_parquet_croid-parquet_croid","cro_type":"DOI","cro_value":"10.1/a","system":"nauvis","created_at":"2024-01-01T00:00:00Z"}`))
	})

	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/a"}]}`))
	outDir := t.TempDir()
	parquetDir := t.TempDir()

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log(), Config{
		CROIDURL:   server.URL,
		ParquetDir: parquetDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok = %d, want 1", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}

	// Check parquet file was created
	pfiles, err := os.ReadDir(parquetDir)
	if err != nil {
		t.Fatalf("read parquet dir: %v", err)
	}
	if len(pfiles) != 1 {
		t.Fatalf("expected 1 parquet file, got %d", len(pfiles))
	}
	if pfiles[0].Name() != "parquet_croid_parquet_croid-parquet_croid.parquet" {
		t.Errorf("filename = %q, want parquet_croid_parquet_croid-parquet_croid.parquet", pfiles[0].Name())
	}
}

func TestRun_WithParquet_DuplicatesSkipped(t *testing.T) {
	// Mock CROID server
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"croid":"test_croid_test_croid_test-test_croid","cro_type":"DOI","cro_value":"10.1/a","system":"nauvis","created_at":"2024-01-01T00:00:00Z"}`))
	})

	inDir := t.TempDir()
	// Two files with the same DOI
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	writeGz(t, inDir, "1.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	outDir := t.TempDir()
	parquetDir := t.TempDir()

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log(), Config{
		CROIDURL:   server.URL,
		ParquetDir: parquetDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d, want 2", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}

	// Only one parquet file should be created (the second DOI is a duplicate)
	pfiles, err := os.ReadDir(parquetDir)
	if err != nil {
		t.Fatalf("read parquet dir: %v", err)
	}
	if len(pfiles) != 1 {
		t.Fatalf("expected 1 parquet file (duplicate skipped), got %d", len(pfiles))
	}
}

func TestRun_WithParquet_DisabledParquetDir(t *testing.T) {
	// Mock CROID server
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"croid":"test_croid_test_croid_test-test_croid","cro_type":"DOI","cro_value":"10.1/test","system":"nauvis","created_at":"2024-01-01T00:00:00Z"}`))
	})

	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/test"}]}`))
	outDir := t.TempDir()

	// No parquet dir specified
	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log(), Config{
		CROIDURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok = %d, want 1", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
}

func TestRun_WithCROID_DisabledCROIDURL(t *testing.T) {
	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/test"}]}`))
	outDir := t.TempDir()

	// No croid URL specified
	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log(), Config{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok = %d, want 1", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
}

func TestRun_WithParquet_AbsolutePath(t *testing.T) {
	// Mock CROID server
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/croid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"croid":"abc_crd_abc_crd-abc_crd","cro_type":"DOI","cro_value":"10.1/abs","system":"nauvis","created_at":"2024-01-01T00:00:00Z"}`))
	})

	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/abs"}]}`))
	outDir := t.TempDir()
	parquetDir := filepath.Join(t.TempDir(), "parquet_out")

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log(), Config{
		CROIDURL:   server.URL,
		ParquetDir: parquetDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok = %d, want 1", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}

	// Check the directory was created
	if _, err := os.Stat(parquetDir); err != nil {
		t.Fatalf("parquet dir should exist: %v", err)
	}
}
