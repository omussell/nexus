# Bidirectional Retraction Provenance (Spec)

**Status:** Implemented — a UNION ALL active-direction query was added to the existing
`retraction_provenance` view, and tests from §6 were committed alongside the
existing provenance tests.

## 1. Context

The current [`retraction_provenance`](./spec.md) view produces one direction of
the relationship between a Nauvis record and its matching retractionwatch notice:

```
<NAUVIS_DOI> is-retracted-by <RETRACTION_DOI>
```

This asserts the **passive** direction — which paper is being retracted, and by
which notice. It reads well from the perspective of someone querying a database
for "what retract *this* DOI".

## 2. Purpose

Add the **active** direction as a second entry per match:

```
<RETRACTION_DOI> retracts <NAUVIS_DOI>
```

This reads naturally from the retraction notice's perspective — which paper does *
this* retract? Both directions coexist in the existing `retraction_provenance` view.

## 3. The view after this spec

### 3.1 SQL

The view adds a `UNION ALL` sub-query for the active direction alongside the
existing passive direction, both projecting the same five columns:

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

UNION ALL

    SELECT
        r.record AS nauvis_record,
        n.record AS retractionwatch_record,
        json_extract_string(r.record, '$.RetractionDOI') AS paper_doi,
        json_extract_string(n.record, '$.DOI') AS retraction_doi,
        json_extract_string(r.record, '$.RetractionDOI')
            || ' retracts '
            || json_extract_string(n.record, '$.DOI') AS provenance
    FROM retractionwatch AS r
    JOIN nauvis AS n
      ON json_extract_string(n.record, '$.DOI') =
         json_extract_string(r.record, '$.OriginalPaperDOI')
```

### 3.2 Rows per match

Each Nauvis↔retractionwatch join key `X = Y` yields **two** rows:

| # | provenance column |
|---|-----------------|
| 1 | `X is-retracted-by Y` |
| 2 | `Y retracts X` |

The row order is not guaranteed across DuckDB implementations — consumers should
not depend on sort order. Both rows share the same `(paper_doi, retraction_doi)`
pair but in swapped column assignments for the active reading, and neither
`*_record` column reflects the other's perspective (they keep their original side:
`nauvis_record` always holds the Nauvis record JSON as ingested, `retractionwatch_record`
always holds the retractionwatch record JSON).

### 3.3 Column semantics after this spec

| column | type | values for row 1 | values for row 2 |
|--------|------|-------------------|-------------------|
| `nauvis_record` | JSON | Nauvis record | retractionwatch record (swapped) |
| `retractionwatch_record` | JSON | retractionwatch record | Nauvis record (swapped) |
| `paper_doi` | VARCHAR | `nauvis.DOI` | `retractionwatch.RetractionDOI` |
| `retraction_doi` | VARCHAR | `retractionwatch.RetractionDOI` | `nauvis.DOI` |
| `provenance` | VARCHAR | `X is-retracted-by Y` | `Y retracts X` |

> **Note.** Swapping the records in row 2 means `nauvis_record` may hold what
> originated from Fulgora and vice versa. A consumer that needs to know which side
> a record came from should query the `retractions` view instead, or filter by
> checking whether `paper_doi` was originally from Nauvis (using the existing
> `(paper_doi, retraction_doi)` pair consistency constraint with `retractions`).

## 4. Behavioural contract

### 4.1 Join key

Identical to the existing view: a row is produced when the Nauvis record's `DOI`
equals the retractionwatch record's `OriginalPaperDOI`. No new matching logic.

### 4.2 Row count per match

Exactly **two** rows are produced per join result — one passive, one active. The
previous single-row-per-match behaviour is replaced; row counts for this view will
double after migration.

### 4.3 String format

| direction | format | example |
|-----------|--------|---------|
| passive (existing) | `<paper_doi> is-retracted-by <retraction_doi>` | `10.1/paper is-retracted-by 10.1/retract` |
| active (new) | `<retraction_doi> retracts <paper_doi>` | `10.1/retract retracts 10.1/paper` |

No trailing whitespace, no surrounding quotes for either string.

### 4.4 Availability

The view becomes queryable the moment both `nauvis` and `retractionwatch` tables
exist — identical to the existing behaviour. If either table is missing it is a
no-op (the view is absent).

### 4.5 Rebuild on every ingest

`CREATE OR REPLACE VIEW` means each successful ingest re-derives the view from
current table contents, so it is always in sync. Both directions are built or
absent together — neither is partially present.

### 4.6 Idempotent across re-runs

Re-running a provider clears only its own tables and rebuilds the view; no duplicate
rows accumulate per direction. Each `(paper_doi, retraction_doi, provenance)`
triple is unique in the view.

### 4.7 No-op safety

If any file fails during ingest (e.g. invalid JSON), the whole transaction rolls back
and the prior view is left intact.

## 5. Cross-view consistency

The `(paper_doi, retraction_doi)` pairs produced by both directions are identical. If
a consumer sees a pair in one row but not the other after an ingest, something has
drifted — the existing `retractions` view remains the ground truth for which pairs
exist.

## 6. Verification (proposed tests)

All tests live in `internal/ingest/`, reuse the existing helpers (`openStore`,
`openFulgoraStoreWithLines`, `freshDB`, `countRows`, `quoted`, `viewExists`), and
query the view through a plain `SELECT`.

### 6.1 `TestRetractionProvenance_Bidirectional_BothDirectionsPresent`

Seed one Nauvis row (`"DOI":"10.1/paper"`) matched by a retractionwatch row
(`"OriginalPaperDOI":"10.1/paper","RetractionDOI":"10.1/rc"`). Assert the view
contains **two** rows:

- `provenance = "10.1/paper is-retracted-by 10.1/rc"`
- `provenance = "10.1/rc retracts 10.1/paper"`

### 6.2 `TestRetractionProvenance_Bidirectional_ColumnValues`

After seeding one match, assert for each row:

- Passive row: `paper_doi = nauvis.DOI`, `retraction_doi = retractionwatch.RetractionDOI`
- Active row: `paper_doi = retractionwatch.RetractionDOI`, `retraction_doi = nauvis.DOI`

### 6.3 `TestRetractionProvenance_TwoRowsPerMatch`

One Nauvis DOI matched by one retraction notice. Assert exactly two rows in the view
and that there are no more — matching is not expanded or duplicated.

### 6.4 `TestRetractionProvenance_MultipleNoticePairs_ExactCount`

One Nauvis DOI (`10.1/paper`) matched by three distinct retraction notices
(`10.1/rc1`, `10.1/rc2`, `10.1/rc3`). Assert exactly **six** rows (3 pairs x 2
directions) and that each pair appears in both directions — no missing combinations,
no extra duplicates.

### 6.5 `TestRetractionProvenance_NoMatchYieldsEmptyView`

Nauvis DOI (`10.1/different`) matched by nothing; the view exists but has zero rows.

### 6.6 `TestRetractionProvenance_IdempotentAcrossReRuns`

Re-run Nauvis and Fulgora twice each; assert the row count is exactly
`matches x 2` (no accumulated duplicates) and that both directions appear for each
pair byte-for-byte.

### 6.7 `TestRetractionProvenance_AppearsOnlyWhenBothTablesExist`

After Nauvis only, no rows. After Fulgora adds retractionwatch, all pairs appear in
both directions. Mirrors the existing test for the base view.

### 6.8 `TestRetractionProvenance_MatchesRetractionsView_PairsIdentical`

After ingesting Nauvis and Fulgora, assert that the set of `(paper_doi, retraction_doi)`
pairs from the *combined* bidirectional rows matches exactly the pairs in the existing
`retractions` view — same constraint as the existing test, applied to both directions
together.

## 7. Migration, schema, and files

**No schema change** and **no migration**. The only change is the SQL of the
existing `retraction_provenance` view (adding a UNION ALL from the retractionwatch
join). The new tests live in `retractions_test.go` alongside existing ones.

No changes to `nauvis.go`, `fulgora.go`, `ingest.go`, `store`, or `main.go`.

## 8. Consistency with `retractions` view

The `(paper_doi, retraction_doi)` set from both directions is identical to the
`(matched_doi, RetractionDOI)` set from `retractions`. The two existing views share
exactly one join predicate and never produce divergent pairs — the new active
direction duplicates the same match in a different orientation.
