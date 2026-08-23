// Package collect runs the fetch workflow for a source: check for a new
// version, skip if it is already collected, otherwise download the raw file,
// process it into a standardized output, and record both paths in the database
// (marking the version as latest).
package collect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/store"
)

// Result is the outcome of one Collect for a single source+version.
type Result struct {
	Source       string
	Version      string
	CollectedAt  string
	InitialInput string
	Output       string
	Skipped      bool // true when the version was already recorded in the database
}

// Collect fetches (or reuses) the latest known version of src and records it
// in the store. rawRoot is the root under which per-source directories live,
// e.g. "systems/fulgora"; the raw file is stored under rawRoot/<source>/
// initial_input and the standardized output under rawRoot/<source>/output.
func Collect(ctx context.Context, src source.Source, s *store.Store, rawRoot string) (*Result, error) {
	latest, err := src.CheckLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect: %s: %w", src.Name(), err)
	}
	if latest.Version == "" {
		return nil, fmt.Errorf("collect: %s: source returned no version", src.Name())
	}

	// If we already collected this exact version, do not download it again.
	if _, err := s.Get(ctx, src.Name(), latest.Version); err == nil {
		rec, err := s.Latest(ctx, src.Name())
		if err != nil {
			return nil, err
		}
		return &Result{
			Source:       rec.Source,
			Version:      rec.Version,
			CollectedAt:  rec.CollectedAt,
			InitialInput: rec.InitialInput,
			Output:       rec.Output,
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

	return &Result{
		Source:       rec.Source,
		Version:      rec.Version,
		CollectedAt:  rec.CollectedAt.UTC().Format(time.RFC3339),
		InitialInput: rec.InitialInput,
		Output:       rec.Output,
	}, nil
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
