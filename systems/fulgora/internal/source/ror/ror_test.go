package ror

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const apiJSON = `{
  "hits": {
    "hits": [
      {
        "id": 8436953,
        "files": [
          {"key": "v1.34-2023-10-12-ror-data.xml",
           "links": {"self": "https://example/api/records/8436953/files/v1.34-2023-10-12-ror-data.xml"}},
          {"key": "v1.34-2023-10-12-ror-data.csv",
           "links": {"self": "https://example/api/records/8436953/files/v1.34-2023-10-12-ror-data.csv"}}
        ]
      },
      {
        "id": 1,
        "files": [{"key": "v1.00-data.csv", "links": {"self": "https://example/api/records/1/files/v1.00-data.csv"}}]
      }
    ]
  }
}`

func newAPIServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/communities/ror-data/records", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(apiJSON)); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCheckLatest(t *testing.T) {
	s := NewWithAPIBase(newAPIServer(t))
	got, err := s.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if got.Version != "1.34" {
		t.Errorf("version = %q, want %q", got.Version, "1.34")
	}
	// The newest file is the LAST one attached to the newest record.
	wantURL := "https://example/api/records/8436953/files/v1.34-2023-10-12-ror-data.csv/content"
	if got.URL != wantURL {
		t.Errorf("url = %q, want %q", got.URL, wantURL)
	}
	if got.Filename != "v1.34-2023-10-12-ror-data.csv" {
		t.Errorf("filename = %q", got.Filename)
	}
}

func TestCheckLatestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := NewWithAPIBase(srv.URL)
	if _, err := s.CheckLatest(context.Background()); err == nil {
		t.Fatal("expected an error on 503")
	}
}

func TestProcessUnpacks(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "v1.34-2023-10-12-ror-data.zip")
	if err := writeZip(raw, map[string]string{
		"ror.csv": "id,display_name\n0000-0001-2229-186X,Institute of Example",
	}); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	outRoot := filepath.Join(dir, "output")
	s := New()
	out, err := s.Process(context.Background(), "1.34", raw, outRoot)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := filepath.Join(outRoot, "1.34")
	if out != want {
		t.Fatalf("output dir = %q, want %q", out, want)
	}
	b, err := os.ReadFile(filepath.Join(want, "ror.csv"))
	if err != nil {
		t.Fatalf("read unpacked: %v", err)
	}
	if string(b) == "" {
		t.Error("unpacked ror.csv is empty")
	}
}

func TestProcessRejectsBadVersion(t *testing.T) {
	if _, err := New().Process(context.Background(), "", "/does/not/matter", t.TempDir()); err == nil {
		t.Fatal("expected an error for empty version")
	}
}

// TestVersionFromFilename pins the label extraction for a few real-world names.
func TestVersionFromFilename(t *testing.T) {
	cases := map[string]string{
		"v1.34-2023-10-12-ror-data.zip": "1.34",
		"v1.63-2025-01-01-ror-data.xml": "1.63",
		"1.63":                          "1.63",
	}
	for in, want := range cases {
		if got := versionFromFilename(in); got != want {
			t.Errorf("versionFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSafeJoin pins the zip-slip guard.
func TestSafeJoinEscapes(t *testing.T) {
	want := filepath.Join("out", "ok.csv")
	got, err := safeJoin("out", "ok.csv")
	if err != nil || got != want {
		t.Fatalf("safeJoin(ok) = %q, %v", got, err)
	}
	if _, err := safeJoin("out", "../evil.csv"); err == nil {
		t.Fatal("expected escape to be rejected")
	}
}

func TestCheckLatestMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	s := NewWithAPIBase(srv.URL)
	if _, err := s.CheckLatest(context.Background()); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

// writeZip creates a zip at path with the given name->content entries.
func writeZip(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return zw.Close()
}
