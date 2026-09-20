# Retraction Provenance Derivation (Spec)

**Status:** Spec only — proposes adding a `retraction_provenance` view to
Vulcanus. No code has been changed; this document describes the intended data,
its derivation from the existing ingest pipeline, and verification.

## 1. Purpose

Vulcanus currently lets a consumer answer one retraction question: *"Is the
retraction watch aware of a retraction for a record I hold in Nauvis?"* via the
derived `retractions` view, whose rows pair each Nauvis record with every matching
`retractionwatch` notice on `DOI = OriginalPaperDOI`.

This spec proposes deriving a second, **flattened, consumer-ready view** that
turns each Nauvis↔retractionmatch into a single string asserting provenance:

```
<NAUVIS_DOI> is-retracted-by <RETRACTION_DOI>
```

In other words, for a given Nauvis DOI that matches a Fulgora retractionwatch
`OriginalPaperDOI`, we emit one row of the form
`<DOI> is-retracted-by <RetractionDOI>`. This is a durable, queryable record of
*which retraction notice (by its own RetractionDOI) retracts which paper*, as
determined by Vulcanus — a provenance assertion that can be exported straight to
CSV/Parquet, joined downstream, or embedded in RDF without per-row string
assembly.

## 2. Location in the pipeline

This work adds a third derived view alongside the existing `retractions` view in
the ingest transaction. `retractions` produces the joined **records**; this view
produces **provenance strings** from exactly those joins. The two share the same
matching key and the same availability precondition — a new author view sits on
top of the same Nauvis↔retractionwatch `JOIN`.

Because it is a *view* over the already-loaded tables (a projection, not a
loaded table), this follows the established pattern in
[`docs/how-it-works.md`](./how-it-works.md): it is always in sync with
`nauvis` and `retractionwatch`, is rebuilt on every ingest, and holds no copy of
the raw bytes.

### 2.1 Where it differs from `retractions`

The `retractions` view exposes full JSON records side by side; this spec
produces a **narrow, typed projection** — the DOI keys plus the composed
string. This is not transformation of any payload's content: no record is
rewritten or pruned. The `X is-retracted-by` string is a derived label over two
already-present JSON-extracted columns, exactly the kind of "link by key that the
existing view already performs" the docs already carve out as the one allowed
exception to Vulcanus's preserve-bytes rule.

## 3. The view

### 3.1 SQL

```sql
CREATE OR REPLACE VIEW retraction_provenance AS
SELECT
    n.record AS nauvis_record,
    r.record AS retractionwatch_record,
    json_extract_string(n.record, '$.DOI') AS paper_doi,
    json_extract_string(r.record, '$.RetractionDOI') AS retraction_doi,
    json_extract_string(n.record, '$.DOI')
        || ' is-retracted-by '
        || json_extract_string(r.record, '$.RetractionDOI') AS provenance
FROM nauvis AS n
JOIN retractionwatch AS r
  ON json_extract_string(n.record, '$.DOI') =
     json_extract_string(r.record, '$.OriginalPaperDOI')
```

The `provenance` column holds the exact request output:
`<DOI> is-retracted-by <RetractionDOI>`.

### 3.2 Columns

| column | type | notes |
|--------|------|-------|
| `nauvis_record` | JSON | the full Nauvis record (Crossref item). Retained for debugging/join context. |
| `retractionwatch_record` | JSON | the full retractionwatch notice. |
| `paper_doi` | VARCHAR | `nauvis.DOI`, the retracted paper's DOI. Equals the join key. |
| `retraction_doi` | VARCHAR | `retractionwatch.RetractionDOI`, the retraction notice's own DOI. |
| `provenance` | VARCHAR | `paper_doi || ' is-retracted-by ' || retraction_doi` — the requested string. |

The `retraction_doi` and `provenance` columns are the primary output. The
`*_record` columns are retained so a consumer who needs more context (title,
retraction date, etc.) never has to re-join — while a consumer who only wants the
string can `SELECT provenance FROM retraction_provenance` directly.

## 4. Behavioural contract

4.1 **Join key.** A row is produced when the Nauvis record's `DOI` equals the
retractionwatch record's `OriginalPaperDOI` — identical matching to the existing
`retractions` view. The provenance string links a **Nauvis** DOI to a
**Fulgora** `RetractionDOI` via the requested `"DOI is-retracted-by RetractionDOI"`
syntax.

4.2 **Shape of the string.** The output is `paper_doi`, a single space, the
literal `is-retracted-by`, a single space, then `retraction_doi`. No trailing
whitespace, no surrounding quotes:
`10.1/yes is-retracted-by 10.1/no`.

4.3 **One row per match.** Multiple retraction notices pointing at the same
Nauvis DOI each produce their own row (one provenance string each), exactly as
the `retractions` view does. A single retracted paper is never collapsed into one
string; notices are not concatenated.

4.4 **Availability.** The view becomes queryable the moment **both** `nauvis` and
`retractionwatch` tables exist, identically to `retractions`. If either table is
missing it is a no-op (the third author view is also absent; neither
`retractions` nor this view is partially present).

4.5 **Rebuild on every ingest.** `CREATE OR REPLACE VIEW` means each successful
ingest — Nauvis or Fulgora, either provider — re-derives the view from current
table contents, so it is always in sync.

4.6 **Idempotent across re-runs.** Re-running a provider clears only its own
tables and rewrites the view idempotently; the result set never accumulates
duplicate rows.

4.7 **No-op safety.** The third author view, like the existing ones, is rebuilt
under the same single transaction as the ingest. If any file fails (e.g. invalid
JSON), the whole run rolls back and the prior view is left intact, preserving
the coexist safety property.

## 5. Consistency with `retractions`

The two views must always agree on matches: the set of `(paper_doi,
retraction_doi)` pairs appearing in `retraction_provenance` is identical to the
set of `(matched_doi, r.RetractionDOI)` pairs in `retractions`. They share one
`JOIN` predicate with the same key. When both the existing `retractions` view and
the new view are queried against the same DuckDB, every `provenance.paper_doi`
must appear as a `retractions.matched_doi`, and every `provenance.retraction_doi`
must pair with that DOI in `retractions`. This constraint — that the author view's
join is *exactly* the existing one and never a superset or subset — is the core of
its behavioural contract and is what the tests in §6 pin down.

## 6. Verification (proposed tests)

All tests live in `internal/ingest/`, reuse the existing helpers (`openStore`,
`openFulgoraStoreWithLines`, `freshDB`, `viewExists`, `countRows`, `quoted`),
and exercise the view through a plain `SELECT`, exactly as
`TestRetractionsView_MatchesByDOI` does for the existing view.

- **`TestRetractionProvenance_BuildsRequestedString`.** Seed one Nauvis row
  (`"DOI":"10.1/yes"`) matched by a retractionwatch row
  (`"OriginalPaperDOI":"10.1/yes","RetractionDOI":"10.1/no"`). Query
  `SELECT provenance, paper_doi, retraction_doi FROM retraction_provenance` and
  assert the single row yields `provenance = "10.1/yes is-retracted-by 10.1/no"`,
  with `paper_doi = "10.1/yes"` and `retraction_doi = "10.1/no"`.
- **`TestRetractionProvenance_NoMatchYieldsEmptyView`.** Nauvis DOI
  (`10.1/different`) matched by nothing; the view exists but has zero rows.
- **`TestRetractionProvenance_MultipleNotices`.** One Nauvis DOI
  (`10.1/yes`) matched by two retractionwatch rows with distinct `RetractionDOI`s
  (`10.1/rc1`, `10.1/rc2`). Assert two rows with distinct `provenance` strings
  and that notices are not concatenated.
- **`TestRetractionProvenance_IdempotentAcrossReRuns`.** Re-run Nauvis and
  Fulgora twice each; assert exactly one row per notice (no duplicates) and that
  the string is byte-for-byte stable.
- **`TestRetractionProvenance_AppearsOnlyWhenBothTablesExist`.** After Nauvis
  only, the view is absent; after Fulgora follows, it appears. This mirrors
  `TestRetractionsView_AppearsOnlyWhenBothTablesExist` and guards the no-op-when-
  missing contract.
- **`TestRetractionProvenance_MatchesRetractionsView`.** After ingesting Nauvis
  and Fulgora, assert the `(paper_doi, retraction_doi)` pairs in
  `retraction_provenance` are exactly the `(matched_doi, retraction_doi)` pairs in
  `retractions`. This pins the cross-view consistency constraint in §5 (that no
  new matching logic was introduced) and prevents the new view from drifting from
  the existing `retractions` join in any future change.

## 7. Migration, schema, and files

There is **no schema change**: no new table or migration is added. The only new
object is the `retraction_provenance` view, created inside the same transaction as
the ingest, using existing helper routines (`tablesExist`, `quoted`). The third
author view lives in `retractions.go` next to `retractionsViewName` and
`buildRetractionsView`; it reuses the existing `buildRetractionsView` function
(add the third view after the existing one) and its tests live in
`retractions_test.go` alongside the existing retraction-view tests. No changes to
`nauvis.go`, `fulgora.go`, `ingest.go`, `store`, or `main.go` are required, and
the `docs/how-it-works.md` and `index.md` "The database" tables are extended to
list `retraction_provenance`.
