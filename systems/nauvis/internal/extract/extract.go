// Package extract reads the Crossref snapshot files: it decompresses a
// gzip-compressed item file, validates that it is a single well-formed JSON
// value, writes the decompressed bytes out as a plain .json file
// byte-for-byte unchanged (so no value or formatting is altered), and reports
// the DOIs of the items it contains.
package extract

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Process decompresses inPath, validates the payload as one JSON value, writes
// it unchanged to outPath, and returns the DOIs of its items in input order.
// n is the number of decompressed bytes written.
func Process(inPath, outPath string) (dois []string, n int64, err error) {
	buf, err := readGzip(inPath)
	if err != nil {
		return nil, 0, err
	}
	// Validate that the payload is a single well-formed JSON value. json.Valid
	// checks syntactic correctness without interpreting values.
	if !json.Valid(buf) {
		return nil, 0, fmt.Errorf("%s: decompressed payload is not valid JSON", inPath)
	}
	if err := atomicWrite(outPath, buf, 0o644); err != nil {
		return nil, 0, fmt.Errorf("write %s: %w", outPath, err)
	}

	var doc struct {
		Items []struct {
			DOI string `json:"DOI"`
		} `json:"items"`
	}
	if err := json.Unmarshal(buf, &doc); err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", inPath, err)
	}
	dois = make([]string, 0, len(doc.Items))
	for _, it := range doc.Items {
		dois = append(dois, it.DOI)
	}
	return dois, int64(len(buf)), nil
}

// Files returns an ordered list of every input file in dir. A file qualifies
// if its name ends in ".json.gz" (e.g. "5.json.gz") or ".json" (e.g.
// "10.json").
func Files(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

// BaseName derives the output filename (a trailing .gz is stripped) for a given
// input path, e.g. "/data/5.json.gz" -> "5.json".
func BaseName(inPath string) string {
	return strings.TrimSuffix(filepath.Base(inPath), ".gz")
}

func readGzip(path string) ([]byte, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, fmt.Errorf("gzip %s: %w", path, err)
	}
	defer gz.Close()
	buf, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return buf, nil
}

// atomicWrite writes data to path, creating the parent directory if needed,
// writing to a temp file in the same directory, then renaming into place.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
