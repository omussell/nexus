package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nexus/croid/internal/db"
	"github.com/nexus/croid/internal/migrate"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "store.sqlite3")+"?_fk=1&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetMaxOpenConns(1)
	if err := migrate.Apply(context.Background(), conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db.New(conn))
}

// TestCreateConcurrentSameIdentity proves that N goroutines racing to create
// the same identity all observe a single, identical CROID (the losers win the
// unique-constraint race then read the winner's row back).
func TestCreateConcurrentSameIdentity(t *testing.T) {
	s := openStore(t)
	id := Identity{CroType: "DOI", CroValue: "10.5555/999", System: "nauvis"}

	const n = 32
	type res struct {
		croid string
		err   error
	}
	out := make(chan res, n)
	for i := 0; i < n; i++ {
		go func() {
			rec, err := s.Create(context.Background(), id)
			if err != nil {
				out <- res{err: err}
				return
			}
			out <- res{croid: rec.Croid}
		}()
	}

	var first string
	for i := 0; i < n; i++ {
		r := <-out
		if r.err != nil {
			t.Fatalf("Create: %v", r.err)
		}
		if first == "" {
			first = r.croid
		} else if r.croid != first {
			t.Fatalf("concurrent Create produced divergent CROIDs: %q vs %q", r.croid, first)
		}
	}
	if first == "" {
		t.Fatal("no CROID returned")
	}
}

// TestCreateReturnsExisting verifies that a second Create for the same
// identity returns the same CROID without minting a new one.
func TestCreateReturnsExisting(t *testing.T) {
	s := openStore(t)
	id := Identity{CroType: "DOI", CroValue: "10.5555/777", System: "nauvis"}

	first, err := s.Create(context.Background(), id)
	if err != nil {
		t.Fatalf("initial Create: %v", err)
	}
	if !first.Created {
		t.Fatal("expected first create to be marked Created")
	}

	second, err := s.Create(context.Background(), id)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.Croid != first.Croid {
		t.Fatalf("same identity returned different CROID: %q vs %q", second.Croid, first.Croid)
	}
	if second.Created {
		t.Error("second Create should not be marked Created")
	}
}

// TestCreateValidation ensures required fields are enforced.
func TestCreateValidation(t *testing.T) {
	s := openStore(t)
	for i, id := range []Identity{
		{System: "n", CroValue: "v"},               // missing cro_type
		{CroType: "DOI", CroValue: "v"},            // missing system
		{CroType: "DOI", System: "n"},              // missing cro_value
		{CroType: " ", System: "n", CroValue: "v"}, // blank cro_type
	} {
		if _, err := s.Create(context.Background(), id); err == nil {
			t.Errorf("case %d: expected error for %+v", i, id)
		}
	}
}
