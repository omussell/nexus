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

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()

	var got []map[string]string
	dec := json.NewDecoder(f)
	for dec.More() {
		var m map[string]string
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode NDJSON line: %v", err)
		}
		got = append(got, m)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close output: %v", err)
	}
	if len(got) != 2 || got[1]["id"] != "roo" || got[1]["note"] != "second" {
		t.Fatalf("unexpected content: %v", got)
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
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()
	var items []string
	dec := json.NewDecoder(f)
	for dec.More() {
		var item string
		if err := dec.Decode(&item); err != nil {
			t.Fatalf("output not valid NDJSON: %v", err)
		}
		items = append(items, item)
	}
	if len(items) != 1 || items[0] != "only" {
		t.Fatalf("unexpected content: %v", items)
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
