# Crossref Work Schema

Nauvis ingests Crossref "work" records. The JSON that lands in `out/` and the
raw `data/*.json.gz` downloads are all produced by `api.crossref.org`, and each
record is a **subset** of the full Crossref deposit XML described by the
`crossref5.5.0` XSD. This schema validates that JSON shape (not the deposit
XML), so the XSD is used only as a reference for field names and vocabulary,
never as a 1:1 template.

Two files live in `systems/nauvis/schemas/`:

| File | Purpose |
|---|---|
| `crossref-work.schema.json` | JSON Schema (draft 2020-12) for a single Crossref work |
| `validate.py` | Small CLI that validates one or more data files against the schema |

## What it models

- **Root is one work object.** It is applied to a single JSON object. For a
  JSONL dump (each line is one work) it is applied once per line.
- **Draft 2020-12.**
- **Permissive by design.** Fields Crossref controls tightly (dates,
  `content-domain`, `resource`, `issn-type`, `author`, `license`, `link`,
  `funder`, …) are closed objects with `additionalProperties: false`. Fields
  that publishers populate themselves (`reference`, `assertion`, `relation`,
  `institution`, `event`) allow extra properties so new publisher-specific keys
  do not break validation.

## Running the validator

From the `schemas/` directory:

```
cd systems/nauvis/schemas
uv run --with "jsonschema>=4" python validate.py crossref-work.schema.json ../out/*.json ../10.json
```

`jsonschema` is not a project dependency, so it is pulled in on the fly with
`uv run --with "jsonschema>=4"`. Pass the schema as the first argument
explicitly so it is never confused with a data file.

Validate the raw gzipped API envelopes too:

```
uv run --with "jsonschema>=4" python validate.py crossref-work.schema.json ../data/*.json.gz
```

### What it accepts

`validate.py` handles the three shapes that appear in this repo:

| Shape | Example | Handling |
|---|---|---|
| JSONL (one work per line) | `out/*.json` | each top-level object validated |
| Single work object | `10.json` | the object validated |
| API response envelope | `data/*.json.gz` | every object in `items[]` validated |

`.gz` files are decompressed automatically.

### Exit code and output

- `0` — every work validated.
- `1` — one or more works failed.
- `2` — no data files were given.

On failure it prints up to 25 sample errors, then a per-file count, and a
histogram of failure signatures (`<path-tail> :: <validator>`) so recurring
problems are easy to spot:

```
[../out/9.json] 5000 works, 1 failed
[../10.json] 1 works, 0 failed

TOTAL 55001 works, 1 failed

== error categories (path :: validator) ==
       1  0/DOI :: pattern
```

## What it validates

- `systems/nauvis/out/0.json` … `10.json` — 5,000 works each (JSONL).
- `systems/nauvis/10.json` — a single pretty-printed work.
- `systems/nauvis/data/*.json.gz` — the raw `{"items": [...]}` API responses.

Across all of them the schema currently reports **0 failures** (110,001 work
objects total).

## Schema structure

### Required top-level fields

A work must contain these 15 fields:

`DOI`, `URL`, `content-domain`, `created`, `deposited`, `indexed`,
`is-referenced-by-count`, `issued`, `prefix`, `reference-count`,
`references-count`, `resource`, `score`, `source`, `type`

Everything else is optional. The root object is closed, so a work must not
contain keys other than the ones declared in `properties`.

### Dates

All date fields (`created`, `issued`, `published`, `accepted`, …) use the
Crossref date object `{ date-parts, date-time?, timestamp? }`. `date-parts` is
an array of arrays of **integer-or-null** (Crossref uses `null` for a missing
month or day, e.g. `[[2024, null]]`), with 1–3 parts per row.

### Enforced vocabulary

- `type` — enum of 22 values (`book`, `journal-article`, `report`, `dataset`, …).
- `issn-type.type` / `isbn-type.type` — `print` | `electronic`.
- `author.sequence` / `editor.sequence` — `first` | `additional`.
- `DOI` — pattern `^10\.[0-9]{4,}/.+$`.
- `ISSN` (top-level) and `issn-type.value` — pattern `^[0-9]{4}-[0-9]{3}[0-9Xx]$`.

### Reusable definitions (`$defs`)

`dateObj`, `contentDomain`, `resourceLink`, `resource`, `journalIssue`,
`issnType`, `isbnType`, `idRef`, `affiliation`, `person`, `author`,
`institution`, `license`, `link`, `reference`, `assertionGroup`, `assertion`,
`funderId`, `funderAward`, `funder`, `updateTo`, `relationItem`, `relation`,
`eventLocation`, `event`

### Fields modelled to match the real API data

A few shapes differ from intuition and are handled to match what
`api.crossref.org` actually returns:

- `reference[].issn-type` / `reference[].isbn-type` are **strings**, not the
  array-of-objects used at the top level.
- `relation` items use `asserted-by` (and require only `id` + `id-type`).
- `funder[].award[]` items may be a plain string (an award number) or an
  object with `award-number` / `award-uri`.
- `event.location` may be a string or an object.
- Affiliation sub-fields (`place`, `department`, `acronym`) may be a string or
  an array, and `affiliation.id` may be a string or an array of
  `{id, id-type, asserted-by}` objects.

## Design notes

The schema favours catching genuine structural errors over rejecting valid
publisher data. Closed objects are used where Crossref itself guarantees a
fixed shape; permissive objects are used where the content is effectively
free-form and defined by the publisher. If the API adds a new top-level field,
the closed root will flag it — that is intentional, so the schema is kept in
sync with the data.
