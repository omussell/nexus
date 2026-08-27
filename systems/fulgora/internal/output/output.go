// Package output converts standardized dataset dumps into single JSON files.
//
// Conversion happens exactly once per source, through JSONFile, which the
// source calls at the end of its Process method. The value passed in is the
// whole dataset parsed into Go, e.g. a slice of records for CSV input or the
// already-parsed JSON array for the JSON dump.
//
// The package is intentionally small: it performs no validation of the value,
// which is the source's responsibility.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writeJSON writes v (marshaled as indented JSON) to a file named ext under
// outRoot, replacing any existing file. The temp-and-rename pattern matches
// source.Download so a failed write never leaves a partial file in place.
func writeJSON(v any, outRoot, name, ext string) (string, error) {
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outRoot, name+ext)

	b, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return "", fmt.Errorf("output: marshal: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(outRoot, "."+name+".json-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("output: write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("output: rename to %s: %w", outPath, err)
	}
	return outPath, nil
}

// JSONFile writes v as a single indented JSON file named <name>.json under
// outRoot and returns the output path.
func JSONFile(v any, outRoot, name string) (string, error) {
	return writeJSON(v, outRoot, name, ".json")
}
