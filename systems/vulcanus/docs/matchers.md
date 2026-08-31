# Matching Extensions: A Pluggable Design

> Status: proposal. No code changes yet.

Vulcanus ingests the "main" Nauvis data into a single `items` table and each
Fulgora source into its own table. The `internal/matching` package holds the
first attempt at *matching* — the algorithms that map Fulgora data onto the
Nauvis data (or onto external referents) to enrich what Nauvis collected.

This document proposes a **pluggable matching architecture** so that `ror` is
the first matching of many. It describes how to turn the existing
`ror_matching.go` prototype into a family of matchers — `retractionwatch`,
`funder`, `preprint`, `orcid`, and so on — behind a single, stable API, without
touching `main.go` or `ingest` for each new matcher.

Read `how-it-works.md` first for how records arrive in DuckDB. Nothing here
changes that. The matching layer reads from the tables `ingest` already wrote.

## What matching means here

From the README, matching is a method for mapping a value to a metadata entry
from another source:

```
citation string -> DOI
journal article -> preprint DOI
funder name   -> ROR ID / Funder ID
affiliation   -> ROR ID
person        -> ORCID
```

`ror_matching` is the prototype: it reads every Nauvis `items` record, extracts
the `DOI`, and writes the collected DOIs into a `ror_matching` table. Its true
purpose is to feed a downstream ROR lookup — map each DOI's funder/affiliation
to a ROR ID. This document focuses on **structuring the code so that purpose
generalises**, not on the ROR lookup itself.

## Guiding principles

1. **Read rows at rest. Preserve the bytes.** Following `ingest`, each matcher
   reads the raw JSON `record` column from DuckDB and never re-serialises it. A
   matcher may extract fields and write *new* tables, but the source tables are
   left untouched.
2. **One matcher per concern, named after the match.** Every matcher owns one
   job (ROR, retractionwatch, funder, …) and one output table. Matchers share a
   common shape but never import each other's results.
3. **A matcher is a data type, not a flag.** New matchers are added by writing a
   new type and registering it once — not by editing `main.go`'s flags or the
   ingest dispatcher.
4. **Run as a library, not a CLI branch.** Matchers are invoked through an
   orchestrator, which `main.go` can call in one line. `main.go` stays a pure
   dispatcher, as it is today.
5. **Each matcher is independently runnable and testable.** Adding matcher X
   requires zero changes to matcher Y, and can be unit-tested in isolation.

## The matcher interface

Every matcher implements one interface with a single method. This is the
contract that lets them be listed, ordered, and run uniformly.

```go
// matcher performs exactly one matching pass over the source tables,
// writing to its own result table(s). It is a value; it holds no state.
type matcher interface {
    // Name identifies the matcher and its output table (e.g. "ror",
    // "retractionwatch"). It must be a valid DuckDB identifier prefix.
    Name() string

    // Run scans the source tables in outDB, writes the matcher's result
    // tables, and returns the number of source records it produced a mapping
    // for. It drops and recreates its own output table(s) on every call, so the
    // table always holds the latest run. Passing an empty table list means
    // "use the default source".
    Run(ctx context.Context, outDB string, tableHints []string) (int, error)
}
```

This mirrors `ingest.Run`'s existing signature (`ctx`, `outDB`, a count and an
error) so a matcher is familiar to anyone who has read `ingest.go`. The extra
`tableHints` argument carries an optional source-table filter (e.g. only run
against a particular Fulgora source); when empty, the matcher reads its
documented defaults. Returning the count keeps the "faithful row counts"
property `ingest` already guarantees — it can be cross-checked against upstream.

### The single `Match` helper

The per-record work — extract fields from a raw JSON `record`, run the mapping,
and optionally write/emit a result — is common to every matcher. Extract it into
one helper so matchers implement nothing but their own policy:

```go
// Match extracts the record payload, invokes f on it, and returns any row f
// produced. The record is delivered as a decoded map (as in ror_matching's
// extractDOI); malformed or unexpected payloads hand f an error without
// aborting the run, so one bad line never kills a matcher.
func Match(record interface{}, f func(map[string]any) ([]resultRow, error)) ([]resultRow, error)
```

`resultRow` is a lightweight carrier: a slice of column values plus the
destination column names. A matcher that emits to a table returns rows the
orchestrator binds; a matcher that only needs to *count* matches returns zero
rows. This keeps `ror_matching`'s existing single-column (`doi`) output as just
one possible shape.

What stays in each matcher (its real work):

- **which source tables to read** — defaults documented per matcher;
- **which fields of the JSON to key on** — e.g. `DOI`, `title`, `funder`;
- **the mapping policy** — clean/normalise a DOI, match a citation string,
  look up a funder name;
- **the result column schema** — e.g. `(doi)`, `(affiliation_id, orcid)`.

What is *not* in each matcher: connection handling, transaction management,
table creation and drops, per-line JSON decoding, count aggregation. `Match`
and the orchestrator own those.

## Package layout

Following the existing `internal/` convention — one package per provider
(`nauvis`, `fulgora`, `ingest`) with small, well-contained logic — each matcher
lives in its own package under `internal/matchers/<name>`. The core interface,
`Match`, and the orchestrator live in `internal/matching`, mirroring how
`ingest.Run` is a single self-contained package.

```
internal/
  matching/            # stable, shared: the interface, Match, orchestrator
    matching.go        # matcher interface, resultRow
    match.go           # Match(...) helper: decode record, run policy, collect rows
    orchestrator.go    # Run(matchers, outDB, filter): ordering, tx, DDL, binds
  matchers/
    ror/               # current prototype, extracted here
      ror_matching.go
      ror_matching_test.go
    retractionwatch/
      retractionwatch.go
      retractionwatch_test.go
    funder/
      ...
    preprint/
    ...
```

The current `internal/matching/ror_matching.go` — the DOI extraction and the
`ror_matching` table — moves verbatim into `internal/matchers/ror/` once it
adapts to the `matcher` interface. Nothing in it changes but its signature and
package: it already decodes a JSON record, cleans the DOI, drops and recreates
its table, and binds one column. The extraction (`extractDOI`, `cleanDOI`)
becomes the ROR matcher's "which fields and policy" — the part that makes it
*ROR* rather than generic.

> Naming note: `internal/matching` (the core) vs `internal/matchers` (the
> family) reads awkwardly. Acceptable swaps: core `matchers`, family
> `matchers/<name>` → `resolve/<name>`, or core `m` / family `matchers`. Pick
> one once the interface is locked.

## The orchestrator

`orchestrator.Run` is the analogue of `ingest.Run` for matching. It ties the
matchers together: it holds one transaction across all of them (atomic, just as
`ingest` does), runs each matcher in a defined order, and lets a matcher create
and populate its own output table.

```
orchestrator.Run(matchers, outDB, filter):
    open the DuckDB (once, one transaction)
    for each matcher in order:
        prepare: CREATE TABLE if not exists (matcher schema); DROP its old table
        n := matcher.Run(ctx, outDB, filter)      # internally uses Match(...)
        emit n (for the summary)
    commit
    return per-matcher counts
```

Two properties matter:

- **Ordering.** Matchers that feed others must run first. ROR produces DOIs
  (or funder IDs) that a funder matcher could join against; if that dependency
  ever exists, the orchestrator orders by declared prerequisites, not by the
  order matchers were listed in. Keep matchers independent — no matcher reads
  another's output table — so ordering stays trivial and correct.
- **One `matcher.Run` call = one matcher's whole output.** A matcher may write
  several tables (e.g. a raw join plus an aggregate), but the orchestrator never
  reaches inside one matcher to bind its rows. The matcher owns its transaction
  block.

## Registration

Matchers are a *known, finite set* — they are authored in this repo, fetched
from external URLs. So registration is a small, explicit map in the matching
package, not a plugin system:

```go
var all = map[string]matcher{
    "ror":             ror.New(),
    "retractionwatch": retractionwatch.New(),
    "funder":          funder.New(),
    "preprint":        preprint.New(),
    "orcid":           orcid.New(),
}

func For(names ...string) ([]matcher, error)  // by name, with default ordering
func List() []string                            // every registered matcher
```

Adding a matcher is five edits, none in `main.go`:

1. Write `internal/matchers/<name>/<name>.go`, implementing `matcher` and using
   `Match`.
2. Write its tests.
3. Import it in `all` and call `New()`.
4. (Optional) document which source tables it reads and which columns it writes.

## Wiring into `main.go`

The matcher layer is invoked from a thin orchestrator command that adds a new
top-level flag — it does **not** branch the existing `-only nauvis|fulgora|all`
switch. `main.go` stays a dispatcher:

```
-go run . -only all [nauvis/fulgora flags]   # unchanged: ingest
go run . -match ror,preprint                 # new: run matchers against the
                                             # already-ingested outDB
```

Matching presupposes `ingest` has run (it reads tables `ingest` wrote), so it is
a separate command rather than a mode of `-only`. `main.go` routes to
`orchestrator.Run` when `-match` is present. That keeps `main.go` unchanged for
every matcher added afterward — a hard requirement given it is pure dispatch.

## The existing prototype under this design

`ror_matching.go` maps onto the design directly:

| current file | role in the design |
|--------------|--------------------|
| `package matching` | becomes `package ror` under `internal/matchers/ror` |
| `Run(ctx, outDB) (int, error)` | becomes `(*Matcher).Run(ctx, outDB, tableHints) (int, error)`, implementing `matcher` |
| `extractDOI` / `cleanDOI` | the ROR matcher's field extraction and DOI-clanning policy (kept in `ror`, not in `Match`) |
| `ror_matching` const + DROP/CREATE/INSERT | executed through `Match`/orchestrator as the matcher's output schema `(doi VARCHAR)` |
| `errDoiKey` | a malformed-payload policy handled per matcher via `Match`'s error contract |

The `ror_matching_test.go` tests also move to `internal/matchers/ror`
essentially unchanged — they already seed an `items` table, call `Run`, and
assert on the returned count and the output table. They continue to prove the
schema, the DOI-prefix stripping, the missing-DOI handling, and the idempotent
re-run.

## Extending beyond the prototype

The design accommodates the README's listed matchers:

```
journal article -> preprint DOI   -> preprint matcher
funder info      -> grant DOI     -> funder matcher (or grant matcher)
funder name      -> ROR/Funder ID -> funder matcher
affiliation      -> ROR ID        -> ror matcher (already)
person           -> ORCID         -> orcid matcher
```

The retractionwatch matcher is the natural second example: it maps a Nauvis
DOI to whether that DOI appears in Fulgora's retractionwatch data, writing a
boolean/flag table. It exercises a different shape than ROR (join across tables
rather than a single-column extraction) and is a good smoke test that the
interface is sufficiently general.

As matchers get richer, keep the boundaries clean:

- **Normalisation** (DOI-prefix stripping, name trimming) stays in the matcher
  that owns the field, in a small helper, not in `Match`.
- **Referential lookups** (ROR, ORCID, funder registries) should resolve to
  local fixtures or files owned by Vulcanus, never to an unauthenticated
  external endpoint. If such data grows, model it as another table the matcher
  reads from `outDB` (ingested the same way), not as an inline assumption.
- **Result schema** is declared per matcher in its `New()`/`Run`, so each
  matcher's output table is self-describing.
- **Ordering** is by declared prerequisites; because matchers must not read
  another matcher's output, the default (list order) is correct and only
  needs renaming if a future cross-matcher dependency is introduced intentionally.

## Testing and verification

- **Unit tests** for each matcher stay in `internal/matchers/<name>/` and test
  only that matcher, exactly as `ror_matching_test.go` tests ROR.
- **A suite test** in `internal/matching` iterates `List()` (or a curated set)
  and runs each through the orchestrator against a seeded `items` table,
  asserting the run returns a count and creates the declared output table — this
  catches a matcher that forgets to implement the interface or the schema.
- Confirm behaviour with the project's tooling from the vulcanus directory,
  e.g. `go vet ./...` and `go test ./...`, before citing a matcher as working.

## Open questions

- **Registration:** explicit map (chosen) vs. a `matchers` builder the caller
  supplies. Map is simpler and matches the "finite set authored here" reality;
  see the naming note above.
- **Interface method count:** should `Run` itself own table creation, or should
  the orchestrator's `prepare` step own `CREATE`/`DROP` uniformly? Owning it in
  the orchestrator is marginally cleaner but pushes "own your output schema"
  responsibility into the matcher regardless.
- **`tableHints`:** is a per-matcher optional source filter worth the argument,
  or should every matcher always read its documented defaults? Cheap to keep;
  can drop if unused.
