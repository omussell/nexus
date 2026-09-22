package ingest

import (
	"context"
	"path/filepath"
	"testing"
)

func TestHasTableIgnoresNonMainSchemas(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	db, err := openDuckDB(filepath.Join(tmp, "test.duckdb"))
	if err != nil {
		t.Fatalf("openDuckDB: %v", err)
	}
	defer db.Close()
	// Keep the pool at one connection so TEMP tables are visible to every
	// query (TEMP tables are per-connection).
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE nauvis (id INTEGER)`); err != nil {
		t.Fatalf("create temp table: %v", err)
	}

	// Premise: duckdb_tables() does report the TEMP table, which is exactly
	// what the old schema-blind query would have matched.
	var seen int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM duckdb_tables() WHERE table_name = 'nauvis'`).Scan(&seen); err != nil {
		t.Fatalf("duckdb_tables lookup: %v", err)
	}
	if seen == 0 {
		t.Skip("TEMP table not visible in duckdb_tables() here; nothing to guard against")
	}
	if hasTable(ctx, db, "nauvis") {
		t.Error("TEMP table must not count as an existing main-schema table")
	}

	if _, err := db.ExecContext(ctx, `CREATE SCHEMA other`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE other.nauvis (id INTEGER)`); err != nil {
		t.Fatalf("create other-schema table: %v", err)
	}
	if hasTable(ctx, db, "nauvis") {
		t.Error("non-main-schema table must not count")
	}

	// tablesExist must apply the same restriction: only TEMP and
	// other-schema nauvis tables exist so far. The transaction is short
	// because the pool allows a single connection.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if exist, err := tablesExist(ctx, tx, "nauvis"); err != nil {
		t.Fatalf("tablesExist: %v", err)
	} else if exist {
		t.Error("tablesExist: TEMP/other-schema tables must not count")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE ror (id INTEGER)`); err != nil {
		t.Fatalf("create main-schema table: %v", err)
	}
	if !hasTable(ctx, db, "ror") {
		t.Error("main-schema table must count")
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE nauvis (id INTEGER)`); err != nil {
		t.Fatalf("create main-schema nauvis: %v", err)
	}
	if !hasTable(ctx, db, "nauvis") {
		t.Error("main-schema table must count even when other schemas have the same name")
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if exist, err := tablesExist(ctx, tx, "nauvis", "ror"); err != nil {
		t.Fatalf("tablesExist: %v", err)
	} else if !exist {
		t.Error("tablesExist: main-schema tables must count")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if hasTable(ctx, db, "does_not_exist") {
		t.Error("missing table must not count")
	}
}

func TestSplitJSONNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"plain", `["A","B"]`, []string{"A", "B"}},
		{"null element skipped", `["A", null, "B"]`, []string{"A", "B"}},
		{"number element skipped", `["A", 42, "B"]`, []string{"A", "B"}},
		{"object element skipped", `["A", {"x":1}, "B"]`, []string{"A", "B"}},
		{"all malformed", `[null, 1, {"x":1}]`, nil},
		{"not an array", `{"a":1}`, nil},
		{"garbage", `not json`, nil},
		{"dedup and trim", `[" A ", "A", " b "]`, []string{"A", "b"}},
		{"blank strings dropped", `["  ", "B"]`, []string{"B"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitJSONNames(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("splitJSONNames(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitJSONNames(%q) = %v, want %v", tc.input, got, tc.want)
				}
			}
		})
	}
}
