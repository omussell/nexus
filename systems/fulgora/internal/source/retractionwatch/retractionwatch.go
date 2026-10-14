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
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/nexus/fulgora/internal/output"
	"github.com/nexus/fulgora/internal/source"
)

const (
	name          = "retractionwatch"
	gitlabAPIBase = "https://gitlab.com"
	project       = "crossref%2Fretraction-watch-data"
	csvPath       = "retraction_watch.csv"
	csvFilename   = "retraction_watch.csv"
	csvRawURL     = gitlabAPIBase + "/crossref/retraction-watch-data/-/raw/main/retraction_watch.csv?ref_type=heads&inline=false"
)

// Source fetches and standardizes the Retraction Watch CSV.
type Source struct {
	apiBase string // GitLab API base, overridable in tests
	client  *http.Client
}

// New returns a Retraction Watch source.
func New() *Source {
	return &Source{apiBase: gitlabAPIBase, client: &http.Client{Timeout: 15 * time.Second}}
}

// NewWithAPIBase returns a source whose GitLab API base is base (testing hook).
func NewWithAPIBase(base string) *Source {
	return &Source{apiBase: base, client: &http.Client{Timeout: 15 * time.Second}}
}

// gitlabCommit is the subset of a GitLab commit we need for the version label.
type gitlabCommit struct {
	CommittedDate string `json:"committed_date"`
}

// CheckLatest asks the GitLab API for the commit that last touched
// retraction_watch.csv and derives the version (its date) and download URL.
// If the API is unreachable (e.g. 403), the fallback version is taken from
// the version of the most recent file already on disk.
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
		// GitLab may return 403 for unauthenticated commit queries.
		// Fall back to the latest version already on disk.
		return s.checkLocalFallback()
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

// checkLocalFallback returns a Latest derived from the latest version
// already on disk. It looks for the most recent CSV under the source's
// initial_input directory relative to the current working directory (which
// is the Fulgora data root when the script runs).
func (s *Source) checkLocalFallback() (*source.Latest, error) {
	files, err := filepath.Glob(filepath.Join(s.Name(), "initial_input", name+"-*.csv"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("retractionwatch: no local files found for fallback")
	}
	// Pick the file with the newest date stamp (last in lexicographic order).
	sort.Strings(files)
	f := files[len(files)-1]
	base := filepath.Base(f)
	// Strip "retractionwatch-<date>_timestamp.csv" -> "<date>"
	base = strings.TrimPrefix(base, name+"-")
	base = strings.SplitN(base, "_", 2)[0]

	return &source.Latest{
		Version:  base,
		URL:      csvRawURL,
		Filename: base + ".csv",
	}, nil
}

// Name implements source.Source.
func (s *Source) Name() string { return name }

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

// RetractionWatchRecord captures fields from a retraction watch entry.
type RetractionWatchRecord struct {
	ArticleType            string `parquet:"article_type,optional"`
	Author                 string `parquet:"author,optional"`
	Country                string `parquet:"country,optional"`
	Institution            string `parquet:"institution,optional"`
	Journal                string `parquet:"journal,optional"`
	Notes                  string `parquet:"notes,optional"`
	OriginalPaperDOI       string `parquet:"original_paper_doi,optional"`
	OriginalPaperDate      string `parquet:"original_paper_date,optional"`
	PaperTitle             string `parquet:"paper_title,optional"`
	PublishedRetractionDOI string `parquet:"published_retraction_doi,optional"`
	RetractionDate         string `parquet:"retraction_date,optional"`
	RetractionDOI          string `parquet:"retraction_doi,optional"`
	RetractionReason       string `parquet:"retraction_reason,optional"`
	RetractionType         string `parquet:"retraction_type,optional"`
}

// ConvertParquet converts the RetractionWatch NDJSON or JSON-array output to Parquet bytes.
func (s *Source) ConvertParquet(data []byte) ([]byte, error) {
	// Try JSON array first.
	var rawArray []json.RawMessage
	if err := json.Unmarshal(data, &rawArray); err == nil {
		var rows []RetractionWatchRecord
		for _, raw := range rawArray {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			r := RetractionWatchRecord{
				ArticleType:            strField(obj, "ArticleType"),
				Author:                 strField(obj, "Author"),
				Country:                strField(obj, "Country"),
				Institution:            strField(obj, "Institution"),
				Journal:                strField(obj, "Journal"),
				Notes:                  strField(obj, "Notes"),
				OriginalPaperDOI:       strField(obj, "OriginalPaperDOI"),
				OriginalPaperDate:      strField(obj, "OriginalPaperDate"),
				PaperTitle:             strField(obj, "PaperTitle"),
				PublishedRetractionDOI: strField(obj, "PublishedRetractionDOI"),
				RetractionDate:         strField(obj, "RetractionDate"),
				RetractionDOI:          strField(obj, "RetractionDOI"),
				RetractionReason:       strField(obj, "RetractionReason"),
				RetractionType:         strField(obj, "RetractionType"),
			}
			rows = append(rows, r)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		return marshalRows(rows)
	}

	// Fallback: NDJSON (one object per line).
	var rows []RetractionWatchRecord
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		r := RetractionWatchRecord{
			ArticleType:            strField(obj, "ArticleType"),
			Author:                 strField(obj, "Author"),
			Country:                strField(obj, "Country"),
			Institution:            strField(obj, "Institution"),
			Journal:                strField(obj, "Journal"),
			Notes:                  strField(obj, "Notes"),
			OriginalPaperDOI:       strField(obj, "OriginalPaperDOI"),
			OriginalPaperDate:      strField(obj, "OriginalPaperDate"),
			PaperTitle:             strField(obj, "PaperTitle"),
			PublishedRetractionDOI: strField(obj, "PublishedRetractionDOI"),
			RetractionDate:         strField(obj, "RetractionDate"),
			RetractionDOI:          strField(obj, "RetractionDOI"),
			RetractionReason:       strField(obj, "RetractionReason"),
			RetractionType:         strField(obj, "RetractionType"),
		}
		rows = append(rows, r)
	}

	if len(rows) == 0 {
		return nil, nil
	}
	return marshalRows(rows)
}

// strField and marshalRows are shared helpers.

func strField(obj map[string]json.RawMessage, key string) string {
	val, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		return ""
	}
	return s
}

func marshalRows(rows []RetractionWatchRecord) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := parquet.NewWriter(buf)
	for i := range rows {
		if err := w.Write(&rows[i]); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
