// Package collect runs the fetch workflow for sources: check for new
// versions, skip if already collected, download the raw file, process it,
// record in the database, mint CROIDs, and optionally write Parquet files.
package collect

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nexus/croid/client"
	"github.com/parquet-go/parquet-go"
	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/sources"
	"github.com/nexus/fulgora/internal/store"
)

// Result is the outcome of one Collect for a single source+version.
type Result struct {
	Source       string
	Version      string
	CollectedAt  string
	InitialInput string
	Output       string
	Croid        string
	Skipped      bool // true when the version was already recorded in the database
}

// Run fetches the latest version of all sources and records them in the store.
// It returns a slice of results, one per source.
//
// If parquetDir is non-empty, a Parquet file is written for each newly
// collected dataset, named {croid}.parquet in that directory.
// If croidClient is non-nil, a CROID is minted for each newly recorded dataset.
func Run(ctx context.Context, s *store.Store, rawRoot, parquetDir string, croidClient *client.Client) ([]Result, error) {
	return RunFiltered(ctx, s, rawRoot, parquetDir, sources.All(), croidClient)
}

// RunFiltered fetches the latest version of the specified sources and records
// them in the store. It returns a slice of results, one per source.
//
// If parquetDir is non-empty, a Parquet file is written for each newly
// collected dataset, named {croid}.parquet in that directory.
// If croidClient is non-nil, a CROID is minted for each newly recorded dataset.
func RunFiltered(ctx context.Context, s *store.Store, rawRoot, parquetDir string, srcNames []string, croidClient *client.Client) ([]Result, error) {

	if parquetDir != "" {
		if err := os.MkdirAll(parquetDir, 0o755); err != nil {
			return nil, fmt.Errorf("collect: create parquet dir: %w", err)
		}
	}

	results := make([]Result, len(srcNames))
	var mu sync.Mutex
	var firstErr error

	var wg sync.WaitGroup
	for i, name := range srcNames {
		i := i
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			src, err := sources.Get(name)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			res, err := Collect(ctx, src, s, rawRoot, parquetDir, croidClient)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			results[i] = *res
			mu.Unlock()
		}()
	}
	wg.Wait()

	return results, firstErr
}

// Collect fetches (or reuses) the latest known version of src and records it
// in the store. rawRoot is the root under which per-source directories live,
// e.g. "systems/fulgora"; the raw file is stored under rawRoot/<source>/
// initial_input and the standardized output under rawRoot/<source>/output.
//
// If croidClient is non-nil, a CROID is minted for the newly recorded dataset.
// If parquetDir is non-empty, a Parquet file is written for the dataset.
func Collect(ctx context.Context, src source.Source, s *store.Store, rawRoot, parquetDir string, croidClient *client.Client) (*Result, error) {
	latest, err := src.CheckLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect: %s: %w", src.Name(), err)
	}
	if latest.Version == "" {
		return nil, fmt.Errorf("collect: %s: source returned no version", src.Name())
	}

	// If we already collected this exact version, do not download it again.
	if rec, err := s.Get(ctx, src.Name(), latest.Version); err == nil {
		return &Result{
			Source:       rec.Source,
			Version:      rec.Version,
			CollectedAt:  rec.CollectedAt.UTC().Format(time.RFC3339),
			InitialInput: rec.InitialInput,
			Output:       rec.Output,
			Croid:        rec.Croid,
			Skipped:      true,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := time.Now().UTC()

	srcDir := filepath.Join(rawRoot, src.Name())
	inputPath := filepath.Join(
		srcDir, "initial_input",
		fmt.Sprintf("%s-%s_%s", src.Name(), latest.Version, stamp(now))+extOf(latest.Filename, ".zip"),
	)
	outputRoot := filepath.Join(srcDir, "output")

	if err := source.Download(ctx, nil, latest.URL, inputPath); err != nil {
		os.Remove(inputPath) // never leave a partial file behind
		return nil, fmt.Errorf("collect: %s: download %s: %w", src.Name(), latest.Version, err)
	}

	outPath, err := src.Process(ctx, latest.Version, inputPath, outputRoot)
	if err != nil {
		return nil, fmt.Errorf("collect: %s: process %s: %w", src.Name(), latest.Version, err)
	}

	rec, err := s.Record(ctx, store.Dataset{
		Source:       src.Name(),
		Version:      latest.Version,
		CollectedAt:  now,
		InitialInput: inputPath,
		Output:       outPath,
	})
	if err != nil {
		return nil, fmt.Errorf("collect: %s: record %s: %w", src.Name(), latest.Version, err)
	}

	// Write consolidated Parquet file alongside the JSON output.
	if parquetDir != "" {
		if _, err := writeConsolidatedParquet(src.Name(), outPath, rawRoot); err != nil {
			fmt.Fprintf(os.Stderr, "collect: %s: write consolidated parquet %s: %v\n", src.Name(), outPath, err)
		}
	}

	// Mint a CROID and write CROID-named Parquet for the newly recorded dataset.
	if croidClient != nil && rec.ID > 0 {
		croid, mintErr := croidClient.MintWith(ctx, "dataset", fmt.Sprintf("%s:%s", src.Name(), latest.Version))
		if mintErr != nil {
			return nil, fmt.Errorf("collect: %s: mint croid %s: %w", src.Name(), latest.Version, mintErr)
		}
		rec.Croid = croid
		if err := s.UpdateCroid(ctx, rec.ID, croid); err != nil {
			return nil, fmt.Errorf("collect: %s: update croid %s: %w", src.Name(), latest.Version, err)
		}

		// Write CROID-named Parquet file if directory is configured.
		if parquetDir != "" {
			if _, err := writeParquet(src.Name(), croid, outPath, parquetDir); err != nil {
				fmt.Fprintf(os.Stderr, "collect: %s: write parquet %s: %v\n", src.Name(), croid, err)
			}
		}
	}

	return &Result{
		Source:       rec.Source,
		Version:      rec.Version,
		CollectedAt:  rec.CollectedAt.UTC().Format(time.RFC3339),
		InitialInput: rec.InitialInput,
		Output:       rec.Output,
		Croid:        rec.Croid,
		Skipped:      false,
	}, nil
}

// writeParquet converts the output file to Parquet and writes it to the
// parquetDir as {croid}.parquet.
func writeParquet(source, croid, outputPath, parquetDir string) (string, error) {
	pb, err := convertParquet(source, outputPath)
	if err != nil {
		return "", err
	}
	pPath := filepath.Join(parquetDir, croid+".parquet")
	if err := os.MkdirAll(parquetDir, 0o755); err != nil {
		return "", err
	}
	tmp := pPath + ".tmp"
	if err := os.WriteFile(tmp, pb, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, pPath); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return pPath, nil
}

// writeConsolidatedParquet converts the output file to Parquet and writes it
// alongside the JSON output file as {outputPath}.parquet.
func writeConsolidatedParquet(source, outputPath, dataRoot string) (string, error) {
	pb, err := convertParquet(source, outputPath)
	if err != nil {
		return "", err
	}
	pPath := filepath.Join(dataRoot, strings.TrimSuffix(outputPath, ".json")) + ".parquet"
	if err := os.MkdirAll(filepath.Dir(pPath), 0o755); err != nil {
		return "", err
	}
	tmp := pPath + ".tmp"
	if err := os.WriteFile(tmp, pb, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, pPath); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return pPath, nil
}

func convertParquet(source string, outputPath string) ([]byte, error) {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("parquet: read %s: %w", outputPath, err)
	}

	switch source {
	case "ror":
		return convertROR(data)
	case "retractionwatch":
		return convertRetractionWatch(data)
	default:
		return convertGeneric(data)
	}
}

// ROROrganization captures the most important fields from a ROR organization record.
type ROROrganization struct {
	ID                string            `parquet:"ror_id,optional"`
	Name              string            `parquet:"name,optional"`
	Type              string            `parquet:"type,optional"`
	CountryCode       string            `parquet:"country_code,optional"`
	Country           string            `parquet:"country,optional"`
	Aliases           []string          `parquet:"aliases"`
	Acronym           string            `parquet:"acronym,optional"`
	Labels            []string          `parquet:"labels"`
	Links             []string          `parquet:"links"`
	ExternalIDs       map[string]string `parquet:"external_ids,optional"`
	EstablishmentDate string            `parquet:"establishment_date,optional"`
}

// RetractionWatchRecord captures fields from a retraction watch entry.
type RetractionWatchRecord struct {
	ArticleType       string `parquet:"article_type,optional"`
	Author            string `parquet:"author,optional"`
	Country           string `parquet:"country,optional"`
	Institution       string `parquet:"institution,optional"`
	Journal           string `parquet:"journal,optional"`
	Notes             string `parquet:"notes,optional"`
	OriginalPaperDOI  string `parquet:"original_paper_doi,optional"`
	OriginalPaperDate string `parquet:"original_paper_date,optional"`
	PaperTitle        string `parquet:"paper_title,optional"`
	PublishedRetractionDOI string `parquet:"published_retraction_doi,optional"`
	RetractionDate    string `parquet:"retraction_date,optional"`
	RetractionDOI     string `parquet:"retraction_doi,optional"`
	RetractionReason  string `parquet:"retraction_reason,optional"`
	RetractionType    string `parquet:"retraction_type,optional"`
}

func convertROR(data []byte) ([]byte, error) {
	var rawArray []json.RawMessage
	if err := json.Unmarshal(data, &rawArray); err != nil {
		return nil, fmt.Errorf("parse ror json array: %w", err)
	}

	var rows []ROROrganization
	for _, raw := range rawArray {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}

		r := ROROrganization{
			ID:   strField(obj, "id"),
			Name: strField(obj, "name"),
			Type: strField(obj, "type"),
		}

		r.CountryCode = strField(obj, "country.code")
		r.Country = strField(obj, "country.name")
		r.Aliases = strArray(obj, "aliases")

		if v := strField(obj, "acronym"); v != "" {
			r.Acronym = v
		}

		r.Labels = strArray(obj, "labels")
		r.Links = strArray(obj, "links")

		if v := strField(obj, "establishment_date"); v != "" {
			r.EstablishmentDate = v
		}

		externalIDs := strMap(obj, "external_ids")
		if len(externalIDs) > 0 {
			r.ExternalIDs = externalIDs
		}

		rows = append(rows, r)
	}

	return marshalParquet(rows)
}

func convertRetractionWatch(data []byte) ([]byte, error) {
	var rawArray []json.RawMessage
	if err := json.Unmarshal(data, &rawArray); err != nil {
		return nil, fmt.Errorf("parse retractionwatch json array: %w", err)
	}

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

	return marshalParquet(rows)
}

func convertGeneric(data []byte) ([]byte, error) {
	var rows []Record

	var jsonRows []map[string]interface{}
	if err := json.Unmarshal(data, &jsonRows); err != nil {
		// Fallback: treat as NDJSON
		lines := bytes.Split(data, []byte{'\n'})
		for _, line := range lines {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal(line, &obj); err != nil {
				continue
			}
			rows = append(rows, Record{Data: string(line)})
		}
		return marshalParquet(rows)
	}

	for _, obj := range jsonRows {
		raw, _ := json.Marshal(obj)
		rows = append(rows, Record{Data: string(raw)})
	}

	return marshalParquet(rows)
}

type Record struct {
	Data string `parquet:"data,optional"`
}

func marshalParquet(rows interface{}) ([]byte, error) {
	buf := &bytes.Buffer{}
	writer := parquet.NewWriter(buf)
	if err := writer.Write(rows); err != nil {
		return nil, fmt.Errorf("parquet: write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("parquet: close: %w", err)
	}
	return buf.Bytes(), nil
}

// stamp is a sortable filesystem-safe timestamp for the initial_input filename
// (e.g. "2025-01-01T00-00-00Z"), matching the examples in the system README.
func stamp(t time.Time) string {
	return t.Format("2006-01-02T15-04-05Z")
}

// extOf returns the extension of name, or fallback if name has none.
func extOf(name, fallback string) string {
	if e := filepath.Ext(name); e != "" {
		return e
	}
	return fallback
}

// --- helpers ---

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

func strArray(obj map[string]json.RawMessage, key string) []string {
	val, ok := obj[key]
	if !ok {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(val, &arr); err != nil {
		return nil
	}
	return arr
}

func strMap(obj map[string]json.RawMessage, key string) map[string]string {
	val, ok := obj[key]
	if !ok {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(val, &m); err != nil {
		return nil
	}
	return m
}
