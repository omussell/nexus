// Package matching defines the matcher contract, the per RecordMatch helper,
// and the orchestrator that runs a batch of matchers over a DuckDB database.
// The concrete matchers live under matchers/<name>.
//
// A Matcher owns a single matching concern — ROR, retractionwatch, funder, ...
// and produces one result table. The orchestrator owns connection handling,
// transactions, output-table creation, row counting and registration; a matcher
// implements only its policy: which source table(s) to read, which JSON fields
// to key on, how to map them, and the schema of its own output table.
package matching

import "context"

// ResultRow is one row of a matcher's output. Columns names the columns the row
// will be bound into; Values holds the corresponding values. The two slices have
// the same length and order.
type ResultRow struct {
	Columns []string
	Values  []any
}

// ResultColumns is the schema of one result table: the ordered column names the
// matcher's Run binds into. It is declared by the matcher, validated and
// materialised by the orchestrator.
type ResultColumns struct {
	Cols []string
}

// Names returns the column name, in order.
func (c ResultColumns) Names() []string { return c.Cols }

// PolicyFunc reads the fields a matcher's concern needs from a decoded record
// and returns the result rows to emit for it. It is the piece each matcher
// supplies to Match; it never touches the database. A nil slice means the record
// produced no mapping; a non-nil error signals a record that is unmatchable in a
// way that must fail the run.
type PolicyFunc func(ctx context.Context, record map[string]any) ([]ResultRow, error)

// Matcher is a single matching concern. It is a value, holds no state, and is
// looked up by its registered name before use.
type Matcher interface {
	// Name is the matcher's identifier and the DuckDB table it writes.
	Name() string

	// Policy returns the function run per source record. It must return a
	// non-nil (empty) slice or nil, never such that the orchestrator panics;
	// nil is tolerated by Match.
	Policy(ctx context.Context) PolicyFunc

	// Columns declares the schema of the result table, which the
	// orchestrator creates and empties before binding.
	Columns(ctx context.Context) (ResultColumns, error)

	// Run executes the matcher against outDB: it scans the source table, binds
	// the emitted rows into the result table, and returns how many were bound.
	// It opens its own connection so a matchers run keeps matchers isolated.
	// A nil result row produced per record (Match's non-object path) counts as
	// zero matches, not as an error.
	Run(ctx context.Context, outDB string) (int, error)
}

// MatcherFunc adapts a function to the Matcher interface.
type MatcherFunc func(ctx context.Context) PolicyFunc

// Match performs the per-record work every matcher shares: it validates that
// record is a JSON object (as scanned from the source table), then passes the
// decoded map to policy, which reads the fields its concern needs and returns
// the rows to emit.
//
// A record that is not an object produces an empty slice and no error. The
// orchestrator counts such rows as zero matches rather than failing, so a
// matcher's strictness lives in its Policy, not in Match.
func Match(record any, policy PolicyFunc) (any, error) {
	m, ok := record.(map[string]any)
	if !ok {
		return nil, nil
	}
	return policy(context.Background(), m)
}
