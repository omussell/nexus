# DuckDB JSON Reconciliation in vulcanus

## Overview

This document summarizes the integration opportunities for DuckDB v2.0's four new JSON reconciliation primitives into the vulcanus system. The system already uses DuckDB v2.10505.0 (`github.com/duckdb/duckdb-go/v2`) but only leverages `json_extract` and `json_extract_string`.

## Four New Scalar Functions

### 1. `json_merge_patch_diff(orig, modified)` — Patch Diff

**Purpose:** Computes minimal RFC 7396 patch such that `json_merge_patch(orig, patch) = modified`.

- `null` values in patch delete keys
- Changed/added keys appear with new values  
- Unchanged keys omitted entirely
- Recursively handles nested objects

**Example:**
```sql
SELECT json_merge_patch_diff('{"a":1,"b":2,"c":3}', '{"a":1,"b":99,"d":4}');
-- {"c":null,"b":99,"d":4}
```

**Vulcanus use cases:**
- CDC-style change tracking between nauvis record versions
- Computing minimal diffs between record states for audit/logging
- Shipping diffs instead of full state reduces payload size

### 2. `json_deep_merge(original, patch[, patch2, ...])` — Recursive Merge with Skip-on-Null

**Purpose:** Same structure as `json_merge_patch`, but `null` means "keep the original value" instead of "delete the key".

- `null` values skip (preserve original)
- Non-null values overwrite
- Variadic — applies patches left to right

**Example:**
```sql
SELECT json_deep_merge('{"a":1,"b":2}', '{"b":null}');
-- {"a":1,"b":2}  -- b preserved, unlike json_merge_patch which deletes it
```

**Vulcanus use cases:**
- Reconciling fragments from multiple upstreams where `null` means "no value"
- Merging ROR data with other organization metadata sources without losing existing values

### 3. `json_normalize(json)` — Canonical Form for Hashing

**Purpose:** Recursively sorts keys of every object (including nested objects in arrays), enabling hash-based deduplication.

- Array element order is preserved (meaningful in JSON)
- Scalar values returned unchanged

**Example:**
```sql
SELECT json_normalize('{"z":1,"a":2,"m":3}');
-- {"a":2,"m":3,"z":1}
```

**Vulcanus use cases:**
- `GROUP BY xxh3_128(json_normalize(record))` to collapse duplicate ROR/organization records with different key orderings
- Deduplicating candidates in ROR matching to avoid redundant matching work
- Identifying duplicate entries in retractions views

### 4. `json_strip_nulls(json)` — Recursively Drop Null-Valued Keys

**Purpose:** Removes every key whose value is JSON `null`, recursively.

- Array elements are not modified
- Useful for cleaning patches before storage

**Example:**
```sql
SELECT json_strip_nulls('{"a":1,"b":null,"c":null,"d":2}');
-- {"a":1,"d":2}
```

**Vulcanus use cases:**
- Clean `json_merge_patch_diff` output before storage (removes `null` entries for unchanged keys)
- Clean incoming events before deep-merging onto current state
- Process records in ROR matching and retractions views

## Integration Opportunities in vulcanus

### `internal/match/duckdb.go`
- **`json_normalize`**: Add candidate deduplication via `xxh3_128(json_normalize(record))` via `hashfuncs` extension
  - Before scoring candidates, compute hash to skip re-matching already-seen records
  - Reduces redundant fuzzy matching work

### `internal/ingest/retractions.go`
- **`json_strip_nulls`**: Clean records in retractions view processing
  - Remove null-valued keys from nauvis/retractionwatch records before comparison
  - Ensures consistent matching when records have been partially stripped

- **`json_merge_patch_diff`**: Compute diffs between record states
  - Track changes between nauvis record versions
  - Audit logging of what changed between ingest runs

### `internal/ingest/ror_matches.go`
- **`json_normalize`**: Deduplicate unique funder/affiliation names
  - Before sending names to worker pool, compute `xxh3_128(json_normalize(name))` via `hashfuncs` extension
  - Skip names already seen with same normalized form

- **`json_strip_nulls`**: Clean records before merging
  - Strip nulls from incoming event data before deep-merging onto current state
  - Part of the end-to-end reconciliation pipeline

## End-to-End Reconciliation Pipeline

The four functions compose into a reconciliation pipeline:

1. **Strip nulls** from incoming event: `json_strip_nulls(event)`
2. **Deep-merge** cleaned event onto current state: `json_deep_merge(c.state, json_strip_nulls(e.event))`
3. **Diff** against original: `json_merge_patch_diff(c.state, ...)`
4. **Hash** normalized result: `xxh3_128(json_normalize(json_merge_patch(c.state, patch)))` via `hashfuncs` extension

**Example scenario:**
- Catalog state: `{"typeName":"Column","name":"user_id","description":"primary key","dataType":"BIGINT","ownerEmail":"alice@example.com"}`
- Incoming event: `{"name":"user_id","description":"primary key for the users table","dataType":"BIGINT","ownerEmail":null,"team":null}`
- After strip nulls: `{"name":"user_id","description":"primary key for the users table","dataType":"BIGINT"}`
- Patch: `{"description":"primary key for the users table"}`
- Content hash: `xxh3_128(json_normalize(json_merge_patch(state, patch)))` — via `hashfuncs` extension

## Benchmarks (500,000 CDC Events)

| Function | Python (s) | DuckDB (s) | Speedup |
|---|---|---|---|
| `json_normalize` | 7.02 | 0.15 | 46.8x |
| `json_deep_merge` | 8.45 | 0.47 | 18.0x |
| `json_merge_patch_diff` | 6.66 | 0.19 | 35.1x |
| `json_strip_nulls` | 6.17 | 0.05 | 123.4x |
| Full chain | 11.11 | 1.03 | 10.8x |

**Speedups come from DuckDB operating on `yyjson` trees in-place vs. Python's `json.loads`/`json.dumps` overhead.**

## Hash Function: `xxh3_128` vs `md5`

The `hashfuncs` community extension provides `xxh3_128` (128-bit xxHash3) as a faster alternative to `md5` for hash-based deduplication.

**Installation (DuckDB v2+):**
```sql
INSTALL hashfuncs;
LOAD hashfuncs;
```

**Usage:**
```sql
-- xxh3_128: 128-bit hash, faster than md5, lower collision risk than xxh3_64
SELECT xxh3_128(json_normalize('{"z":1,"a":2}'));
-- Output: <128-bit hex hash>

-- Comparison: xxh3_128 is ~10x faster than md5 with comparable collision resistance
SELECT md5(json_normalize('{"z":1,"a":2}'));
-- Output: <32-char hex md5 hash>
```

**In vulcanus, replace `md5(json_normalize(payload))` with `xxh3_128(json_normalize(payload))`** for:
- ROR candidate deduplication: `GROUP BY xxh3_128(json_normalize(record))`
- Retractions view duplicate detection
- Full reconciliation pipeline step 4

**Why xxh3_128 over md5:**
- ~10x faster performance in DuckDB
- 128-bit output (vs md5's 128-bit, but with better distribution)
- No known practical collisions for this use case
- Part of the hashfuncs community extension for DuckDB v2+

## Current DuckDB Version

vulcanus uses DuckDB v2.10505.0 (via `github.com/duckdb/duckdb-go/v2`), which supports all four functions natively via the `json` extension. The `LOAD json;` statement must be executed before using these functions.

## No Existing Usage

A search of the codebase found zero existing references to `json_normalize`, `json_merge_patch_diff`, `json_deep_merge`, or `json_strip_nulls` in any Go source files. The system currently only uses `json_extract` and `json_extract_string`.