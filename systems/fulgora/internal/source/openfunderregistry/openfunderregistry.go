// Package openfunderregistry implements the Fulgora source for the Crossref
// Open Funder Registry (funderNames endpoint), distributed as a CSV listing
// all funder names and their DOIs.
//
// The CSV has no explicit version number; the upstream dataset is rewritten
// in place. We use the current date as the version label.
//
// CSV URL:
//
//	https://doi.crossref.org/funderNames?mode=list
package openfunderregistry

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexus/fulgora/internal/output"
	"github.com/nexus/fulgora/internal/source"
)

const (
	name     = "openfunderregistry"
	sourceURL = "https://doi.crossref.org/funderNames?mode=list"
)

// Source fetches and standardizes the Open Funder Registry CSV.
type Source struct {
	client *http.Client
}

// New returns an Open Funder Registry source.
func New() *Source {
	return &Source{client: &http.Client{Timeout: 30 * time.Second}}
}

// Name implements source.Source.
func (s *Source) Name() string { return name }

// CheckLatest always returns the current date as the version and the
// funderNames URL. Because the CSV is updated in place by Crossref, this
// means the version changes on each new day; if the source has not been
// collected today it will be downloaded.
func (s *Source) CheckLatest(ctx context.Context) (*source.Latest, error) {
	return &source.Latest{
		Version:  time.Now().UTC().Format("2006-01-02"),
		URL:      sourceURL,
		Filename: "funderNames.csv",
	}, nil
}

// Process reads the CSV at rawPath, converts each row to a JSON object, and
// writes an NDJSON file under outRoot named "openfunderregistry-<version>.json".
func (s *Source) Process(ctx context.Context, version, rawPath, outRoot string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("openfunderregistry: empty version")
	}
	if rawPath == "" {
		return "", fmt.Errorf("openfunderregistry: empty raw path")
	}
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}

	cols, records, err := parseCSV(rawPath)
	if err != nil {
		return "", fmt.Errorf("openfunderregistry: parse csv %s: %w", rawPath, err)
	}

	rows := make([]map[string]string, 0, len(records))
	for _, rec := range records {
		row := make(map[string]string, len(cols))
		for i, col := range cols {
			if col == "" {
				continue
			}
			if i < len(rec) {
				row[col] = rec[i]
			}
		}
		rows = append(rows, row)
	}

	return output.JSONFile(rows, outRoot, fmt.Sprintf("openfunderregistry-%s", version))
}

// parseCSV reads the CSV at path, returning the header columns and the slice of
// record column-value slices.
func parseCSV(path string) ([]string, [][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("openfunderregistry: empty csv")
	}

	cols := records[0]
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	return cols, records[1:], nil
}

// recordPath is the filename for the raw CSV download (used by tests).
var recordPath = "funderNames.csv"

// versionFromFilename extracts the date stamp from a funderNames raw filename.
func versionFromFilename(fn string) string {
	base := filepath.Base(fn)
	// Strip timestamp: "openfunderregistry-2026-01-01_2026-01-02T10-30-00Z_funderNames.csv"
	if idx := strings.Index(base, "_"); idx > 0 {
		base = base[:idx]
	}
	// Strip source prefix: "openfunderregistry-2026-01-01" -> "2026-01-01"
	base = strings.TrimPrefix(base, name+"-")
	return base
}

func init() {
	// Ensure the record path constant is used; this is only needed if a test
	// wants to reference it. The const is sufficient for normal operation.
}

// --- test helpers ---

// downloadJSONPath returns the expected output path for a given version.
func downloadJSONPath(outRoot, version string) string {
	return filepath.Join(outRoot, fmt.Sprintf("openfunderregistry-%s.json", version))
}

// --- JSON helpers used in tests ---

// funderRow is the shape of one row in the NDJSON output.
type funderRow struct {
	URI              string `json:"uri"`
	PrimaryNameDisplay string `json:"primary_name_display"`
	Replaced         string `json:"replaced"`
}

// funderRecords is the NDJSON output parsed into typed structs.
func funderRecords(data []byte) ([]funderRow, error) {
	var rows []funderRow
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var r funderRow
		if err := dec.Decode(&r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}
