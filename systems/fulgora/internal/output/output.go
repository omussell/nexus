// Package output converts standardized dataset dumps into individual objects written
// to disk as NDJSON (one JSON object per line).
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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// writeJSON writes v as NDJSON (one compact JSON object per line) to a file
// named <name>.json under outRoot, replacing any existing file. The
// temp-and-rename pattern matches source.Download so a failed write never
// leaves a partial file in place.
func writeJSON(v any, outRoot, name string) (string, error) {
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}

	lines, err := encodeLines(v)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(outRoot, "."+name+".json-*")
	if err != nil {
		return "", fmt.Errorf("output: create temp: %w", err)
	}
	defer os.Remove(tmp.Name())

	w := bufio.NewWriterSize(tmp, 8192)
	for _, line := range lines {
		if _, err := w.Write(line); err != nil {
			tmp.Close()
			return "", fmt.Errorf("output: write temp: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("output: flush temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("output: close temp: %w", err)
	}

	tmpPath := tmp.Name()
	finalPath := filepath.Join(outRoot, name+".json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("output: rename temp: %w", err)
	}
	return finalPath, nil
}

// JSONFile writes v as NDJSON (one compact JSON object per line) to a file
// named <name>.json under outRoot and returns the output path.
func JSONFile(v any, outRoot, name string) (string, error) {
	return writeJSON(v, outRoot, name)
}

// encodeLines serializes v as NDJSON lines. It accepts a slice/array, a single
// struct/map, or a top-level map of values, emitting one compact line each.
func encodeLines(v any) ([][]byte, error) {
	if v == nil {
		return [][]byte{}, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return [][]byte{}, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([][]byte, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			line, err := encodeValue(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out = append(out, line)
		}
		return out, nil
	case reflect.Map:
		out := make([][]byte, 0, rv.Len())
		for _, key := range rv.MapKeys() {
			line, err := encodeValue(rv.MapIndex(key).Interface())
			if err != nil {
				return nil, err
			}
			out = append(out, line)
		}
		return out, nil
	default:
		line, err := encodeValue(v)
		if err != nil {
			return nil, err
		}
		return [][]byte{line}, nil
	}
}

// encodeValue marshals a single value as one compact JSON line plus newline.
func encodeValue(v interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return nil, fmt.Errorf("output: marshal: %w", err)
	}
	return buf.Bytes(), nil
}