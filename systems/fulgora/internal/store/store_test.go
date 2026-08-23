package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nexus/fulgora/internal/db"
	"github.com/nexus/fulgora/internal/migrate"
	"github.com/nexus/fulgora/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "store.sqlite3")+"?_fk=1&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.New(db.New(conn))
}

func TestRecordAndLatest(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	_, err := s.Record(ctx, store.Dataset{
		Source:       "ror",
		Version:      "1.63",
		CollectedAt:  now,
		InitialInput: "systems/fulgora/ror/initial_input/ror-1.63_2025-01-01T00-00-00Z.zip",
		Output:       "systems/fulgora/ror/output/1.63",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	latest, err := s.Latest(ctx, "ror")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.Version != "1.63" || latest.Source != "ror" {
		t.Fatalf("latest = %+v", latest)
	}

	// A newer version should become the new latest.
	_, err = s.Record(ctx, store.Dataset{
		Source:       "ror",
		Version:      "1.64",
		CollectedAt:  now.Add(24 * time.Hour),
		InitialInput: "systems/fulgora/ror/initial_input/ror-1.64_2025-01-02T00-00-00Z.zip",
		Output:       "systems/fulgora/ror/output/1.64",
	})
	if err != nil {
		t.Fatalf("Record 1.64: %v", err)
	}
	latest, err = s.Latest(ctx, "ror")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.Version != "1.64" {
		t.Fatalf("latest version = %q, want 1.64", latest.Version)
	}

	// Both versions should be listed by source.
	bySource, err := s.ListBySource(ctx, "ror")
	if err != nil {
		t.Fatalf("ListBySource: %v", err)
	}
	if len(bySource) != 2 {
		t.Fatalf("ListBySource len = %d, want 2", len(bySource))
	}
}

func TestRecordRejectsDuplicate(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	d := store.Dataset{Source: "ror", Version: "1.63", CollectedAt: time.Now().UTC()}
	if _, err := s.Record(ctx, d); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if _, err := s.Record(ctx, d); err == nil {
		t.Fatal("expected an error on duplicate (source, version)")
	}
}

func TestListLatestMixedSources(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, src := range []store.Dataset{
		{Source: "ror", Version: "1.63", CollectedAt: now},
		{Source: "retractionwatch", Version: "2026-08-15", CollectedAt: now},
	} {
		if _, err := s.Record(ctx, src); err != nil {
			t.Fatalf("Record %s: %v", src.Source, err)
		}
	}
	got, err := s.ListLatest(ctx)
	if err != nil {
		t.Fatalf("ListLatest: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListLatest len = %d, want 2", len(got))
	}
	// Ordered by source name: retractionwatch < ror? No: "retractionwatch" < "ror" (e < o). So retractionwatch first.
	if got[0].Source != "retractionwatch" {
		t.Fatalf("first source = %q, want retractionwatch", got[0].Source)
	}
}

func TestGet(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := s.Record(ctx, store.Dataset{Source: "ror", Version: "1.63", CollectedAt: now}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Get(ctx, "ror", "1.63")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != "1.63" {
		t.Fatalf("Get version = %q", got.Version)
	}
	if _, err := s.Get(ctx, "ror", "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Get missing should be sql.ErrNoRows, got %v", err)
	}
}
