package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONFileWritesSingleDocument(t *testing.T) {
	dir := t.TempDir()
	records := []map[string]string{
		{"id": "roi", "note": "hello"},
		{"id": "roo", "note": "second"},
	}

	out, err := JSONFile(records, dir, "mydata")
	if err != nil {
		t.Fatalf("JSONFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "mydata.json" {
		t.Fatalf("wrote wrong files: %v", entries)
	}
	if out != filepath.Join(dir, "mydata.json") {
		t.Fatalf("path = %q", out)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var got []map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(got) != 2 || got[1]["id"] != "roo" {
		t.Fatalf("unexpected content: %s", b)
	}
}

func TestJSONFileCreatesNestedDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	out, err := JSONFile([]string{"only"}, sub, "data")
	if err != nil {
		t.Fatalf("JSONFile: %v", err)
	}
	if filepath.Dir(out) != sub {
		t.Fatalf("output dir = %q", filepath.Dir(out))
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var items []string
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(items) != 1 || items[0] != "only" {
		t.Fatalf("unexpected content: %s", b)
	}
}

// TestJSONFileNoPartialOnMarshalFailure ensures a failed marshal never leaves a
// stray temp file behind.
func TestJSONFileNoPartialOnMarshalFailure(t *testing.T) {
	dir := t.TempDir()
	if _, err := JSONFile(chan int(nil), dir, "bad"); err == nil {
		t.Fatalf("JSONFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files (temp cleaned up), got %v", entries)
	}
}
