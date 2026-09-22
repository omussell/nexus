# ROR Matching — Bug Report

Review of the ROR matching implementation in Vulcanus
(`internal/ingest/ror_matches.go`, `internal/match/`, `main.go`).
`go vet ./...` and `go test ./...` pass; risky DuckDB semantics were verified
empirically against DuckDB 1.1.3. Findings are ordered by severity.

## Critical

### 1. Confidence threshold checks the wrong value

`internal/match/matching.go:114-117` (funder) and `:149-152` (affiliation):

```go
best := ChooseCandidate(fund, scored)
if best == nil || best.Score < 96.0 {
    return nil
}
```

When ≥2 candidates have a score, `ChooseCandidate` calls `Rescore`
(`matching.go:500`), which **overwrites each candidate's `Score` in place with
a pairwise win count** (`matching.go:485`), in the range `[0, len-1]`. The
"96.0 fuzzy-confidence" check therefore compares a win count to 96, which can
only pass with ≥97 candidates where the winner beats every other candidate.

Net effect: any name with ≥2 candidate orgs (the common case — the LIKE prefix
query fetches up to 200 candidates, `duckdb.go:69`) is almost always rejected.
Only names with a single scored candidate ever match.

The test suite documents the mutation as intentional
(`matching_test.go:1130-1136`: "Scores should be replaced with pairwise
comparison scores"), so the threshold check — not the rescore — is the bug.

Fix: check the original fuzzy score ≥ 96 before calling `ChooseCandidate`, or
make `Rescore` non-mutating (return new `CandidateMatch` values).

## High

### 2. Country data is a hard dependency but never used

`internal/ingest/ror_matches.go:45-50`:

```go
resDir := filepath.Join(filepath.Dir(dbPath), "resources")
countryFile := resDir + "/countries.txt"
countries, err := country.Load(countryFile)
if err != nil {
    return fmt.Errorf("load countries from %s: %w", countryFile, err)
}
```

`RunRORMatches` hard-fails if `resources/countries.txt` is missing, with the
path resolved relative to `filepath.Dir(dbPath)` — so an absolute dbPath
elsewhere (e.g. a container) breaks it. But `FindCountries`
(`matching.go:165`) is never called in the pipeline, and `MatchFunder` /
`MatchAffiliation` are always invoked with `fundCountries = nil`
(`ror_matches.go:121,123`). The country data is loaded and passed to
`NewDuckDBClient` but never used for matching — a hard dependency on unused
data that can crash the run.

Fix: either wire country restrictions through (call `FindCountries` per name)
or drop the dependency.

## Medium

### 3. Progress-reporter goroutine leak

`internal/ingest/ror_matches.go:140-152`:

```go
go func() {
    for {
        select {
        case <-reportTicker.C:
            ...
        }
    }
}()
```

No exit path: the goroutine keeps printing (stale) progress every 5 seconds
forever after `RunRORMatches` returns, holding the ticker. Also on the first
tick with `p == 0`, `rate` is 0 and `remaining := float64(totalJobs-int(p)) / rate`
divides by zero → `+Inf`/`NaN` is printed.

Fix: give the reporter a stop channel (closed after `wg.Wait()`); guard `rate != 0`.

### 4. `Reopen` panics on open failure

`internal/match/duckdb.go:36-42`:

```go
func Reopen(dbPath string) *Client {
    conn, err := duckdb.NewConnector(dbPath, nil)
    if err != nil {
        panic(fmt.Errorf("open DuckDB at %s: %w", err))
    }
    ...
}
```

Each of the up to 8 worker goroutines opens its own DuckDB connection via
`match.NewDuckDBClient` (`ror_matches.go:115`). Any lock contention,
unreadable path, or bad file `panic`s the entire process with an unrecoverable
goroutine panic instead of surfacing an error to `RunRORMatches`.

Fix: propagate the error (`NewDuckDBClient` should return `(*Client, error)`
or the worker should recover and report).

### 5. Alignment coordinates are inconsistent across candidates

`internal/match/fuzz.go:49-60`: `FindPartialRatioAlignment` swaps
source/target when the fund string is shorter than the candidate name, so
`SrcStart`/`SrcEnd` are positions in the *longer* string. `ScoreCandidate`
(`matching.go:421-423`) stores them in `CandidateMatch.Start/End` as if they
were positions in the fund string, and `IsBetter`'s position tie-break
(`matching.go:452-457`) compares `Start`/`End` across candidates with
incompatible coordinate systems.

Impact: tie-break ordering only; scores are unaffected.

### 6. `hasTable` uses `duckdb_tables()`, which includes non-main-schema tables

`internal/ingest/ror_matches.go:315-320`. Verified empirically: a TEMP table
is returned by `duckdb_tables()`. A stray temp (or other-schema) table named
`nauvis` or `ror` passes the check, then fails confusingly later.
`tablesExist` in `internal/ingest/retractions.go:94` does this correctly with
`information_schema.tables WHERE table_schema = 'main'`.

Fix: use the same `table_schema = 'main'` check.

### 7. `splitJSONNames` drops all names on any parse failure

`internal/ingest/ror_matches.go:274-293`: if any element of
`$.funder[*].name` / `$.author[*].affiliation[*].name` is non-string (or the
array shape is off), `json.Unmarshal` fails and the function returns `nil` —
the whole paper's funder (or affiliation) names are silently discarded.
Verified: DuckDB 1.1.x yields `[]` (not NULL) for missing keys, so the normal
path is fine; only malformed values trigger this.

Fix: parse per-element (or use `json.RawMessage`) so one bad value doesn't
wipe the rest.

## Low / dead code

- `rormatch` struct (`ror_matches.go:19-26`) — defined, never used.
- `FindCountries`, `ExpandSuffix`, `CheckLatin`, `TokenSortRatio` — no
  production callers (tests only). `ExpandSuffix`'s doc comment claims it is
  "applied during matching" — it is not.
- `CandidateNameMatchExclusion` is always called with `fundCountries = nil`
  (`matching.go:405,407`), making the country-bypass branch
  (`matching.go:349-354`) dead.
- `deaccent` table maps θ/φ/ψ to themselves (`normalize.go:144,147,148`) —
  never transliterated, inconsistent with the rest of the table (safe:
  self-maps return immediately, no infinite loop).
- `main.go:98` mixes `fmt.Printf` (stdout) with `log` (stderr).

## Verified non-issues

- `queryNauvisRecords` JSON-wildcard SQL works as intended in DuckDB 1.1.x
  (tested: partial/missing keys → `[]`; `CAST(... AS VARCHAR)` → valid JSON
  arrays that `splitJSONNames` parses).
- Concurrent DuckDB connections (main read connection + 8 worker connections +
  later write connection) coexist fine on this platform (tested with
  overlapping read-write connections). Worth re-checking on Linux containers,
  the deploy target.
- No data race on the shared match maps (mutex-guarded); `go vet` clean.

## Suggested fix order

1. Threshold: compare the original fuzzy score ≥ 96 before `ChooseCandidate`
   rescores (or make `Rescore` non-mutating) — restores actual matching.
2. Wire country restrictions through (`FindCountries` per name) or drop the
   `countries.txt` hard dependency.
3. Give the reporter goroutine a stop channel; guard `rate != 0`.
4. Make `Reopen`/`NewDuckDBClient` return errors; propagate through the worker
   loop.
5. Cheap cleanups: `rormatch`, dead helpers, `hasTable` schema restriction,
   `splitJSONNames` per-element tolerance.
