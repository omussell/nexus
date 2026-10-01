// Package validate loads the Crossref Work JSON Schema and validates
// raw JSON records against it before they are sent to CROID.
package validate

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed crossref-work.schema.json
var crossrefSchema []byte

// resolved holds the pre-compiled, reference-resolved schema.
var resolved = mustResolveSchema()

// mustResolveSchema unmarshals the embedded schema, resolves references,
// and panics on failure.
func mustResolveSchema() *jsonschema.Resolved {
	var schema jsonschema.Schema
	if err := json.Unmarshal(crossrefSchema, &schema); err != nil {
		panic(fmt.Sprintf("validate: failed to parse schema: %v", err))
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(fmt.Sprintf("validate: failed to resolve schema: %v", err))
	}
	return resolved
}

// Validate checks record against the Crossref Work JSON Schema.
// It returns nil when the record is valid, or an error describing
// the first validation failure.
func Validate(record []byte) error {
	var v interface{}
	if err := json.Unmarshal(record, &v); err != nil {
		return fmt.Errorf("validate: record is not valid JSON: %w", err)
	}
	if err := resolved.Validate(v); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}
