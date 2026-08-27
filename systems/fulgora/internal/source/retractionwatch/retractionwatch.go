// Package retractionwatch implements the Fulgora source for the Retraction
// Watch dataset, distributed by the Crossref community as a single CSV.
//
// The dataset has no explicit version number: the CSV is rewritten in place on
// the "main" branch of the gitlab.com/crossref/retraction-watch-data project.
// We use the commit date of the latest change to retraction_watch.csv as the
// version label.
//
// CSV URL:
//
//	https://gitlab.com/crossref/retraction-watch-data/-/raw/main/retraction_watch.csv
package retractionwatch

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nexus/fulgora/internal/output"
	"github.com/nexus/fulgora/internal/source"
)

const (
	name          = "retractionwatch"
	gitlabAPIBase = "https://gitlab.com"
	project       = "crossref%2Fretraction-watch-data"
	csvPath       = "retraction_watch.csv"
	csvFilename   = "retraction_watch.csv"
	csvRawURL     = gitlabAPIBase + "/crossref/retraction-watch-data/-/raw/main/retraction_watch.csv"
)

// Source fetches and standardizes the Retraction Watch CSV.
type Source struct {
	apiBase string // GitLab API base, overridable in tests
	client  *http.Client
}

// New returns a Retraction Watch source.
func New() *Source {
	return &Source{apiBase: gitlabAPIBase, client: &http.Client{Timeout: 60 * time.Second}}
}

// NewWithAPIBase returns a source whose GitLab API base is base (testing hook).
func NewWithAPIBase(base string) *Source {
	return &Source{apiBase: base, client: &http.Client{Timeout: 60 * time.Second}}
}

// Name implements source.Source.
func (s *Source) Name() string { return name }

// gitlabCommit is the subset of a GitLab commit we need for the version label.
type gitlabCommit struct {
	CommittedDate string `json:"committed_date"`
}

// CheckLatest asks the GitLab API for the commit that last touched
// retraction_watch.csv and derives the version (its date) and download URL.
func (s *Source) CheckLatest(ctx context.Context) (*source.Latest, error) {
	u := fmt.Sprintf("%s/api/v4/projects/%s/repository/commits?path=%s&per_page=1&simplified=true",
		s.apiBase, project, csvPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retractionwatch: query gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("retractionwatch: query gitlab: unexpected status %s", resp.Status)
	}

	var commits []gitlabCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, fmt.Errorf("retractionwatch: decode gitlab response: %w", err)
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("retractionwatch: no commits found for %s", csvPath)
	}

	ver, err := commitDate(commits[0].CommittedDate)
	if err != nil {
		return nil, err
	}

	return &source.Latest{
		Version:  ver,
		URL:      csvRawURL,
		Filename: csvFilename,
	}, nil
}

// Process places the raw CSV into a versioned output file under outRoot and
// returns that file path.
func (s *Source) Process(ctx context.Context, version, rawPath, outRoot string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("retractionwatch: empty version")
	}
	if rawPath == "" {
		return "", fmt.Errorf("retractionwatch: empty raw path")
	}
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return "", err
	}

	// Convert the CSV rows to a slice of maps, then write a single JSON document.
	cols, records, err := parseCSV(rawPath)
	if err != nil {
		return "", fmt.Errorf("retractionwatch: parse csv %s: %w", rawPath, err)
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
	return output.JSONFile(rows, outRoot, fmt.Sprintf("retractionwatch-%s", version))
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
		return nil, nil, fmt.Errorf("empty csv")
	}
	cols := records[0]
	for i := range cols {
		cols[i] = strings.Trim(cols[i], " \t\r\n")
	}
	return cols, records[1:], nil
}

// commitDate parses a GitLab committed_date and reduces it to a YYYY-MM-DD
// version label.
func commitDate(raw string) (string, error) {
	t, err := parseDate(raw)
	if err != nil {
		return "", fmt.Errorf("retractionwatch: parse commit date %q: %w", raw, err)
	}
	return t.UTC().Format("2006-01-02"), nil
}

func parseDate(raw string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000+00:00",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date format %q", raw)
}
