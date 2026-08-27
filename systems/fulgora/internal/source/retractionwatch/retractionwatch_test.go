package retractionwatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commitsJSON = `[
  {"id": "aab", "committed_date": "2026-08-15T09:30:00Z"},
  {"id": "bbb", "committed_date": "2026-07-01T00:00:00Z"}
]`

func newGitlabServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/crossref%2Fretraction-watch-data/repository/commits",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(commitsJSON)); err != nil {
				t.Errorf("write: %v", err)
			}
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCheckLatest(t *testing.T) {
	s := NewWithAPIBase(newGitlabServer(t))
	got, err := s.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	if got.Version != "2026-08-15" {
		t.Errorf("version = %q, want %q", got.Version, "2026-08-15")
	}
	if got.Filename != "retraction_watch.csv" {
		t.Errorf("filename = %q", got.Filename)
	}
	if got.URL != csvRawURL {
		t.Errorf("url = %q, want %q", got.URL, csvRawURL)
	}
}

func TestCheckLatestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()
	s := NewWithAPIBase(srv.URL)
	if _, err := s.CheckLatest(context.Background()); err == nil {
		t.Fatal("expected an error on 400")
	}
}

func TestProcessConvertsCSV(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "input", "retraction_watch.csv")
	if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(raw, []byte("id,title\ndoi1,Title A\n,Title B;\n"), 0o644); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	s := New()
	outRoot := filepath.Join(dir, "output")
	out, err := s.Process(context.Background(), "2026-08-15", raw, outRoot)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := filepath.Join(outRoot, "retractionwatch-2026-08-15.json")
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
	// Only a JSON file should be produced; no legacy CSV in the output dir.
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	csvs, files := 0, 0
	for _, e := range entries {
		files++
		if strings.HasSuffix(e.Name(), ".csv") {
			csvs++
		}
	}
	if files != 1 || csvs != 0 {
		t.Fatalf("want exactly one .json and no .csv in output, got csv=%d files=%d", csvs, files)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var gotRecords []map[string]string
	if err := json.Unmarshal(b, &gotRecords); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(gotRecords) != 2 {
		t.Fatalf("expected 2 records, got %d", len(gotRecords))
	}
	if gotRecords[0]["id"] != "doi1" || gotRecords[0]["title"] != "Title A" {
		t.Errorf("unexpected first record: %v", gotRecords[0])
	}
	if gotRecords[1]["title"] != "Title B;" {
		t.Errorf("unexpected second record: %v", gotRecords[1])
	}
}

func TestCommitDateParsing(t *testing.T) {
	cases := map[string]string{
		"2026-08-15T09:30:00Z":      "2026-08-15",
		"2026-08-15T09:30:00.123Z":  "2026-08-15",
		"2026-08-15T09:30:00+02:00": "2026-08-15",
	}
	for in, want := range cases {
		got, err := commitDate(in)
		if err != nil {
			t.Fatalf("commitDate(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("commitDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommitDateUnrecognized(t *testing.T) {
	if _, err := commitDate("not-a-date"); err == nil {
		t.Fatal("expected an error for unrecognized date")
	}
}

func TestProcessRejectsBadVersion(t *testing.T) {
	if _, err := New().Process(context.Background(), "", "/does/not/matter", t.TempDir()); err == nil {
		t.Fatal("expected an error for empty version")
	}
}
