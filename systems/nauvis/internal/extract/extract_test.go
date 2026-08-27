package extract

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustGzip t-fails if b can't be gzipped, otherwise returns the compressed bytes.
func mustGzip(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// TestProcess_NDJSON verifies that Process writes each input item on its own line
// as NDJSON, compacting the original formatting while preserving content.
func TestProcess_NDJSON(t *testing.T) {
	src := []byte(`{"items":[{"DOI":"10.1/1"},{"DOI":"10.1/2"}]}`)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json.gz")
	if err := os.WriteFile(in, mustGzip(t, src), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "out.json")

	_, _, err := Process(in, out)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "{\"DOI\":\"10.1/1\"}\n{\"DOI\":\"10.1/2\"}\n"
	if string(got) != want {
		t.Fatalf("\n got:  %q\nwant: %q", got, want)
	}
}

// TestProcess_RejectsInvalidJSON ensures non-JSON payloads are rejected
// and no output is written.
func TestProcess_RejectsInvalidJSON(t *testing.T) {
	src := []byte("this is not json at all")
	dir := t.TempDir()
	in := filepath.Join(dir, "bad.json.gz")
	if err := os.WriteFile(in, mustGzip(t, src), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "bad.json")

	_, _, err := Process(in, out)
	if err == nil {
		t.Fatalf("Process should have failed on invalid JSON")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("output file should not exist on failure")
	}
}

// TestProcess_RejectsNonGzip ensures a non-gzip input is rejected.
func TestProcess_RejectsNonGzip(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "plain.json.gz")
	if err := os.WriteFile(in, []byte("{\"not\": \"gzipped\"}"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "plain.json")

	if _, _, err := Process(in, out); err == nil {
		t.Fatalf("Process should have failed on non-gzip input")
	}
}

// TestBaseName verifies .gz is stripped from the basename only when present.
func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/data/0.json.gz":  "0.json",
		"/data/10.json.gz": "10.json",
		"/x/y/5.json":      "5.json",
		"/x/y/5.json.gz":   "5.json",
		"noext":            "noext",
		"noext.gz":         "noext",
	}
	for in, want := range cases {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProcess_RealData is a smoke test that runs the real pipeline against
// the sample files in data/, if they exist in the repo. It verifies NDJSON
// output that matches the decompressed source, one item per line.
func TestProcess_RealData(t *testing.T) {
	sampleDir := "data"
	if _, err := os.Stat(sampleDir); err != nil {
		t.Skipf("sample dir %q not present: %v", sampleDir, err)
	}
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		t.Fatalf("read sample dir: %v", err)
	}
	dir := t.TempDir()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json.gz") {
			continue
		}
		in := filepath.Join(sampleDir, e.Name())
		out := filepath.Join(dir, strings.TrimSuffix(e.Name(), ".gz"))

		// Compute the expected NDJSON output ourselves before running so we
		// can compare, and read back the DOIs to verify.
		data, err := readGzip(in)
		if err != nil {
			t.Fatalf("read sample %s: %v", e.Name(), err)
		}
		if !json.Valid(data) {
			t.Fatalf("sample %s decompressed payload is not valid JSON", e.Name())
		}

		var doc struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse sample %s: %v", e.Name(), err)
		}
		want := buildNDJSON(t, doc.Items)

		got, _, err := Process(in, out)
		if err != nil {
			t.Fatalf("Process(%s): %v", e.Name(), err)
		}
		if len(got) != len(doc.Items) {
			t.Fatalf("Process(%s): got %d DOIs, want %d", e.Name(), len(got), len(doc.Items))
		}
		gotb, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read output %s: %v", e.Name(), err)
		}
		if string(gotb) != want {
			t.Fatalf("output %s differs from expected NDJSON\n got:  %s\nwant: %s", e.Name(), gotb, want)
		}
	}
}

func buildNDJSON(t *testing.T, items []json.RawMessage) string {
	t.Helper()
	var w bytes.Buffer
	for _, item := range items {
		line := new(bytes.Buffer)
		if err := json.Compact(line, item); err != nil {
			t.Fatalf("compact: %v", err)
		}
		w.Write(line.Bytes())
		w.WriteByte('\n')
	}
	return w.String()
}
