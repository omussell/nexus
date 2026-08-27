package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexus/nauvis/internal/store"
)

func log() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, conn, err := store.Open(context.Background(), t.TempDir()+"/test.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return st
}

func writeGz(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestRun_Basic(t *testing.T) {
	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/a"},{"DOI":"10.1/b"}]}`))
	writeGz(t, inDir, "1.json.gz", []byte(`{"items":[{"DOI":"10.1/c"}]}`))
	outDir := t.TempDir()

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 2, log())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d, want 2", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
	if _, err := os.Stat(filepath.Join(outDir, "0.json")); err != nil {
		t.Fatalf("output 0.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "1.json")); err != nil {
		t.Fatalf("output 1.json missing: %v", err)
	}
}

func TestRun_DuplicateDOIs(t *testing.T) {
	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	writeGz(t, inDir, "1.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	outDir := t.TempDir()

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d, want 2", ok)
	}
	if failed != 0 {
		t.Fatalf("failed = %d, want 0", failed)
	}
}

func TestRun_DoesNotRecordSameDoITwice(t *testing.T) {
	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	writeGz(t, inDir, "1.json.gz", []byte(`{"items":[{"DOI":"10.1/dup"}]}`))
	outDir := t.TempDir()

	st := openStore(t)
	Run(context.Background(), inDir, outDir, st, 1, log())
	item, err := st.GetByDOI(context.Background(), "10.1/dup")
	if err != nil {
		t.Fatalf("GetByDOI: %v", err)
	}
	if item.Doi != "10.1/dup" {
		t.Fatalf("Doi = %q", item.Doi)
	}
}

func TestRun_InvalidJSONFails(t *testing.T) {
	inDir := t.TempDir()
	writeGz(t, inDir, "0.json.gz", []byte("not json"))
	writeGz(t, inDir, "1.json.gz", []byte(`{"items":[{"DOI":"10.1/a"}]}`))
	outDir := t.TempDir()

	ok, failed, err := Run(context.Background(), inDir, outDir, openStore(t), 1, log())
	if ok != 1 {
		t.Fatalf("ok = %d, want 1", ok)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}
	if err == nil {
		t.Fatalf("expected aggregate error")
	}
	if !strings.Contains(err.Error(), "1 of 2") {
		t.Fatalf("error = %v", err)
	}
}

func TestRun_Empty(t *testing.T) {
	inDir := t.TempDir()
	ok, failed, err := Run(context.Background(), inDir, t.TempDir(), openStore(t), 0, log())
	if ok != 0 || failed != 0 || err != nil {
		t.Fatalf("Run = (%d,%d,%v), want (0,0,nil)", ok, failed, err)
	}
}
