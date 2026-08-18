package main

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

// TestProcessFile_ByteIdentical verifies that ProcessFile writes the decompressed
// payload back out byte-for-byte — no re-serialization, no type coercion.
func TestProcessFile_ByteIdentical(t *testing.T) {
	src := []byte(`{"a": 1, "b" : [ 1 , 2 , 3 ], "c" : "x"}`)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json.gz")
	if err := os.WriteFile(in, mustGzip(t, src), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "out.json")

	n, err := ProcessFile(in, out)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if n != int64(len(src)) {
		t.Fatalf("returned size %d != expected %d", n, len(src))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("output differs from decompressed source:\n got:  %s\n want: %s", got, src)
	}
}

// TestProcessFile_RejectsInvalidJSON ensures non-JSON payloads are rejected
// and no output is written.
func TestProcessFile_RejectsInvalidJSON(t *testing.T) {
	src := []byte("this is not json at all")
	dir := t.TempDir()
	in := filepath.Join(dir, "bad.json.gz")
	if err := os.WriteFile(in, mustGzip(t, src), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "bad.json")

	_, err := ProcessFile(in, out)
	if err == nil {
		t.Fatalf("ProcessFile should have failed on invalid JSON")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("output file should not exist on failure")
	}
}

// TestProcessFile_RejectsNonGzip ensures a non-gzip input is rejected.
func TestProcessFile_RejectsNonGzip(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "plain.json.gz")
	if err := os.WriteFile(in, []byte("{\"not\": \"gzipped\"}"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	out := filepath.Join(dir, "plain.json")

	if _, err := ProcessFile(in, out); err == nil {
		t.Fatalf("ProcessFile should have failed on non-gzip input")
	}
}

// TestBaseName verifies .gz is stripped from the basename only when present.
func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/data/0.json.gz":       "0.json",
		"/data/10.json.gz":      "10.json",
		"/x/y/5.json":           "5.json",
		"/x/y/5.json.gz":        "5.json",
		"noext":                 "noext",
		"noext.gz":              "noext",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestJSONValid exercises the small wrapper we expose over json.Valid.
func TestJSONValid(t *testing.T) {
	if !jsonValid([]byte(`{"x": 1}`)) {
		t.Errorf("jsonValid should accept valid JSON")
	}
	if jsonValid([]byte(`{x: 1}`)) {
		t.Errorf("jsonValid should reject invalid JSON")
	}
}

// TestProcessFile_RealData is a smoke test that runs the real pipeline against
// the sample files in data/, if they exist in the repo. It verifies byte-identical
// output and valid JSON output for every sample.
func TestProcessFile_RealData(t *testing.T) {
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

		// Compute expected bytes ourselves before running so we can compare.
		expected, err := readGzip(in)
		if err != nil {
			t.Fatalf("read sample %s: %v", e.Name(), err)
		}
		if !json.Valid(expected) {
			t.Fatalf("sample %s decompressed payload is not valid JSON", e.Name())
		}

		n, err := ProcessFile(in, out)
		if err != nil {
			t.Fatalf("ProcessFile(%s): %v", e.Name(), err)
		}
		if n != int64(len(expected)) {
			t.Fatalf("ProcessFile(%s): size %d != %d", e.Name(), n, len(expected))
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read output %s: %v", e.Name(), err)
		}
		if !bytes.Equal(got, expected) {
			t.Fatalf("output %s differs from decompressed source", e.Name())
		}
	}
}

// readGzip is a small local helper (we can't call the package function on
// an un-gzipped path since it would try to write).
func readGzip(path string) ([]byte, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(gz); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
