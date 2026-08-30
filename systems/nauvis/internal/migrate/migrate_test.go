package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSchema(t *testing.T) {
	if !strings.Contains(Schema(), "CREATE TABLE IF NOT EXISTS nauvis") {
		t.Fatalf("Schema missing nauvis table:\n%s", Schema())
	}
}

func TestApply_CreatesTable(t *testing.T) {
	conn := conn(t)
	if err := Apply(context.Background(), conn); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows, err := conn.Query(`PRAGMA table_info(nauvis)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name, typ string
			notNull   bool
			defltRaw  any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defltRaw, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name != "id" && name != "file" && name != "doi" {
			t.Errorf("unexpected column %q", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
}

func TestApply_Idempotent(t *testing.T) {
	conn := conn(t)
	if err := Apply(context.Background(), conn); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := Apply(context.Background(), conn); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
}

func conn(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", t.TempDir()+"/schema.db?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
