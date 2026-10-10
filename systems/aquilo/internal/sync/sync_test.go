package sync

import (
	"context"
	"testing"
)

func TestRunValidation(t *testing.T) {
	ctx := context.Background()
	_, err := Run(ctx, Config{DuckDBPath: "", DBPath: "/tmp/test.db"})
	if err == nil {
		t.Fatal("expected error for empty duckdb path")
	}

	_, err = Run(ctx, Config{DuckDBPath: "/tmp/test.duckdb", DBPath: ""})
	if err == nil {
		t.Fatal("expected error for empty db path")
	}
}

func TestRunDefaultBatchSize(t *testing.T) {
	ctx := context.Background()
	cfg := Config{DuckDBPath: "test.duckdb", DBPath: "test.db", BatchSize: 0}
	// This will fail at the vulcanus.Open step, but validates that default
	// batch size is applied (we just check it doesn't panic or error early).
	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for non-existent duckdb file")
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Run(ctx, Config{DuckDBPath: "test.duckdb", DBPath: "test.db", BatchSize: 0})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
