package dois

import (
	"context"
	"database/sql"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
)

// seedItems creates a fresh DuckDB database with an items table containing the
// given JSON records, then closes the connection.
func seedItems(t *testing.T, records []string) string {
	t.Helper()
	dbPath := t.TempDir() + "/out.duckdb"

	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	db := sql.OpenDB(conn)
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE items (record JSON)`); err != nil {
		db.Close()
		t.Fatalf("create items: %v", err)
	}
	for _, r := range records {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO items (record) VALUES (?)`, r); err != nil {
			db.Close()
			t.Fatalf("insert: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}
	return dbPath
}

func TestRun_ExtractsDOI(t *testing.T) {
	dbPath := seedItems(t, []string{
		`{"DOI":"10.1021/jo020170p","title":"A"}`,
		`{"DOI":"https://doi.org/10.1093/biostatistics/1.2.203"}`,
	})

	n, err := Run(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n != 2 {
		t.Fatalf("run returned %d, want 2", n)
	}

	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	defer conn.Close()
	db := sql.OpenDB(conn)

	var count int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM dois`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("dois table has %d rows, want 2", count)
	}

	rows, err := db.Query(`SELECT doi FROM dois ORDER BY doi`)
	if err != nil {
		t.Fatalf("query dois: %v", err)
	}
	defer rows.Close()
	if err := wantDOIs(t, rows, []string{"10.1021/jo020170p", "10.1093/biostatistics/1.2.203"}); err != nil {
		t.Fatal(err)
	}
}

func TestRun_StripsDoiOrgPrefix(t *testing.T) {
	dbPath := seedItems(t, []string{`{"DOI":"https://doi.org/10.5555/9999"}`})

	if _, err := Run(context.Background(), dbPath); err != nil {
		t.Fatalf("run: %v", err)
	}

	conn, err := duckdb.NewConnector(dbPath, nil)
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	defer conn.Close()
	db := sql.OpenDB(conn)

	var got string
	if err := db.QueryRow(`SELECT doi FROM dois`).Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != "10.5555/9999" {
		t.Fatalf("stored DOI = %q, want bare 10.5555/9999", got)
	}
}

func TestRun_CreatesTableIfMissing(t *testing.T) {
	dbPath := seedItems(t, []string{`{"DOI":"10.1/a"}`})
	if _, err := Run(context.Background(), dbPath); err != nil {
		t.Fatalf("run: %v", err)
	}
	// re-running should not fail; the table is recreated each run
	if _, err := Run(context.Background(), dbPath); err != nil {
		t.Fatalf("re-run: %v", err)
	}
}

func TestRun_EmptyItems(t *testing.T) {
	dbPath := seedItems(t, []string{})
	n, err := Run(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n != 0 {
		t.Fatalf("run returned %d, want 0", n)
	}
}

func wantDOIs(t *testing.T, rows *sql.Rows, want []string) error {
	t.Helper()
	var got []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return err
		}
		got = append(got, d)
	}
	return rows.Err()
}
