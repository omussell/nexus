package migrate

import (
	"context"
	"database/sql"
	"embed"
)

//go:embed schema.sql
var schemaFS embed.FS

var schemaSql []byte = func() []byte {
	b, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		panic(err)
	}
	return b
}()

// Schema returns the raw SQL DDL.
func Schema() string { return string(schemaSql) }

// Apply runs the idempotent schema (CREATE TABLE IF NOT EXISTS) against conn.
func Apply(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, string(schemaSql))
	return err
}
