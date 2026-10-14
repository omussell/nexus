package openfunderregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	s := New()
	if s.Name() != "openfunderregistry" {
		t.Errorf("Name() = %q, want %q", s.Name(), "openfunderregistry")
	}
}

func TestCheckLatest(t *testing.T) {
	s := New()
	got, err := s.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}
	expected := time.Now().UTC().Format("2006-01-02")
	if got.Version != expected {
		t.Errorf("version = %q, want %q", got.Version, expected)
	}
	if got.URL != sourceURL {
		t.Errorf("url = %q, want %q", got.URL, sourceURL)
	}
	if got.Filename != "funderNames.csv" {
		t.Errorf("filename = %q, want %q", got.Filename, "funderNames.csv")
	}
}

func TestProcessConvertsCSV(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "input", "funderNames.csv")
	if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	csvContent := `uri,primary_name_display,replaced
http://dx.doi.org/10.13039/100004343,"3M",
http://dx.doi.org/10.13039/100001436,"1675 Foundation",
http://dx.doi.org/10.13039/100006323,"AACCI Foundation",R
`
	if err := os.WriteFile(raw, []byte(csvContent), 0o644); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	s := New()
	outRoot := filepath.Join(dir, "output")
	ver := "2026-01-01"
	out, err := s.Process(context.Background(), ver, raw, outRoot)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := filepath.Join(outRoot, "openfunderregistry-2026-01-01.json")
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}

	// Only a JSON file should be produced; no legacy CSV in the output dir.
	entries, err := os.ReadDir(outRoot)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	csvs, jsons := 0, 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".csv") {
			csvs++
		}
		if strings.HasSuffix(e.Name(), ".json") {
			jsons++
		}
	}
	if jsons != 1 || csvs != 0 {
		t.Fatalf("want exactly one .json and no .csv in output, got csv=%d json=%d", csvs, jsons)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	records, err := funderRecords(b)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].URI != "http://dx.doi.org/10.13039/100004343" {
		t.Errorf("first URI = %q", records[0].URI)
	}
	if records[0].PrimaryNameDisplay != "3M" {
		t.Errorf("first name = %q", records[0].PrimaryNameDisplay)
	}
	if records[2].Replaced != "R" {
		t.Errorf("replaced flag for 3rd record = %q", records[2].Replaced)
	}
}

func TestProcessDropsEmptyHeaderColumn(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "input", "funderNames.csv")
	if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Trailing comma leaves an empty final column name, which must not appear
	// as an "" key in the NDJSON output.
	csvContent := `uri,primary_name_display,
http://dx.doi.org/10.13039/100004343,3M
`
	if err := os.WriteFile(raw, []byte(csvContent), 0o644); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	s := New()
	out, err := s.Process(context.Background(), "2026-01-01", raw, filepath.Join(dir, "output"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var records []map[string]string
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var rec map[string]string
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("output is not valid NDJSON: %v", err)
		}
		records = append(records, rec)
	}
	for _, rec := range records {
		if _, bad := rec[""]; bad || len(rec) != 2 {
			t.Fatalf("empty header column leaked into output: %v", rec)
		}
	}
}

func TestProcessRejectsBadVersion(t *testing.T) {
	if _, err := New().Process(context.Background(), "", "/does/not/matter", t.TempDir()); err == nil {
		t.Fatal("expected an error for empty version")
	}
}

func TestProcessRejectsEmptyRawPath(t *testing.T) {
	if _, err := New().Process(context.Background(), "1.0", "", t.TempDir()); err == nil {
		t.Fatal("expected an error for empty raw path")
	}
}

func TestCheckLatestDownload(t *testing.T) {
	// Verify the raw CSV can be downloaded from the real endpoint.
	s := New()
	latest, err := s.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("CheckLatest: %v", err)
	}

	resp, err := s.client.Get(latest.URL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %s", resp.Status)
	}
	// Read at least some data to confirm the response is non-empty.
	// Chunked responses report ContentLength as -1, so we read the body.
	var buf bytes.Buffer
	n, err := buf.ReadFrom(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if n == 0 {
		t.Fatal("empty response body")
	}
	if buf.Len() < 100 {
		t.Fatalf("response too small: %d bytes", buf.Len())
	}
}

func TestVersionFromFilename(t *testing.T) {
	cases := map[string]string{
		"openfunderregistry-2026-01-01_funderNames.csv":          "2026-01-01",
		"openfunderregistry-2026-10-13_funderNames.csv":         "2026-10-13",
		"openfunderregistry-2025-05-01_2025-05-01T12-00-00Z_funderNames.csv": "2025-05-01",
	}
	for in, want := range cases {
		if got := versionFromFilename(in); got != want {
			t.Errorf("versionFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertParquetFunderRegistry(t *testing.T) {
	s := New()
	ndjson := `{"uri":"http://dx.doi.org/10.13039/100004343","primary_name_display":"3M","replaced":""}` + "\n" + `{"uri":"http://dx.doi.org/10.13039/100006323","primary_name_display":"AACCI Foundation","replaced":"R"}`

	pb, err := s.ConvertParquet([]byte(ndjson))
	if err != nil {
		t.Fatalf("ConvertParquet: %v", err)
	}
	if len(pb) == 0 {
		t.Fatal("expected non-empty Parquet bytes")
	}
}

func TestConvertParquetFunderRegistryEmpty(t *testing.T) {
	s := New()
	pb, err := s.ConvertParquet([]byte(""))
	if err != nil {
		t.Fatalf("ConvertParquet: %v", err)
	}
	if pb != nil {
		t.Fatal("expected nil for empty input")
	}
}
