---
name: duckdb-json
description: Functions which improve the performance when working with JSON in DuckDB
---

# DuckDB JSON Reconciliation Skills

This skill explains the four new JSON primitives introduced in DuckDB v2.0 for reconciling JSON data between systems.

## Four New Scalar Functions

### `json_merge_patch_diff(orig, modified)` — The Inverse of `json_merge_patch`

Computes the minimal RFC 7396 patch such that `json_merge_patch(orig, patch) = modified`.

- `null` values in the patch delete keys
- Changed/added keys appear with new values
- Unchanged keys are omitted entirely
- Recursively handles nested objects
- Useful for CDC pipelines — shipping diffs instead of full state reduces payload size

**Example:**
```sql
SELECT json_merge_patch_diff('{"a":1,"b":2,"c":3}', '{"a":1,"b":99,"d":4}');
-- {"c":null,"b":99,"d":4}
```

### `json_deep_merge(original, patch[, patch2, ...])` — Recursive Merge with Skip-on-Null

Same recursive merge structure as `json_merge_patch`, but `null` in the patch means "keep the original value" instead of "delete the key".

- `null` values skip (preserve original)
- Non-null values overwrite
- Variadic — applies patches left to right
- Useful for reconciling fragments from multiple upstreams that use `null` to mean "no value"

**Example:**
```sql
SELECT json_deep_merge('{"a":1,"b":2}', '{"b":null}');
-- {"a":1,"b":2}  -- b preserved, unlike json_merge_patch which deletes it
```

### `json_normalize(json)` — Canonical Form for Hashing

Recursively sorts keys of every object (including nested objects in arrays), enabling hash-based deduplication of semantically-equal documents with different key orderings.

- Array element order is preserved (meaningful in JSON)
- Scalar values returned unchanged

**Example:**
```sql
SELECT json_normalize('{"z":1,"a":2,"m":3}');
-- {"a":2,"m":3,"z":1}
```

Enables `GROUP BY md5(json_normalize(payload))` to collapse duplicate emissions.

### `json_strip_nulls(json)` — Recursively Drop Null-Valued Keys

Removes every key whose value is JSON `null`, recursively.

- Array elements are not modified
- Useful for cleaning patches before storage (removes `null` entries from `json_merge_patch_diff`)

**Example:**
```sql
SELECT json_strip_nulls('{"a":1,"b":null,"c":null,"d":2}');
-- {"a":1,"d":2}
```

## End-to-End Reconciliation Workflow

The four functions compose into a reconciliation pipeline:

1. **Strip nulls** from incoming event: `json_strip_nulls(event)`
2. **Deep-merge** cleaned event onto current state: `json_deep_merge(c.state, json_strip_nulls(e.event))`
3. **Diff** against original: `json_merge_patch_diff(c.state, ...)`
4. **Hash** normalized result: `md5(json_normalize(json_merge_patch(c.state, patch)))`

**Example scenario:**
- Catalog state: `{"typeName":"Column","name":"user_id","description":"primary key","dataType":"BIGINT","ownerEmail":"alice@example.com"}`
- Incoming event: `{"name":"user_id","description":"primary key for the users table","dataType":"BIGINT","ownerEmail":null,"team":null}`
- After strip nulls: `{"name":"user_id","description":"primary key for the users table","dataType":"BIGINT"}`
- Patch: `{"description":"primary key for the users table"}`
- Content hash: `md5(json_normalize(json_merge_patch(state, patch)))`

## Benchmarks (500,000 CDC Events)

| Function | Python (s) | DuckDB (s) | Speedup |
|---|---|---|---|
| `json_normalize` | 7.02 | 0.15 | 46.8x |
| `json_deep_merge` | 8.45 | 0.47 | 18.0x |
| `json_merge_patch_diff` | 6.66 | 0.19 | 35.1x |
| `json_strip_nulls` | 6.17 | 0.05 | 123.4x |
| Full chain | 11.11 | 1.03 | 10.8x |

Speedups come from DuckDB operating on `yyjson` trees in-place vs. Python's `json.loads`/`json.dumps` overhead.

## Trying the Functions

```sql
LOAD json;

SELECT json_normalize('{"z":1,"a":2}');
SELECT json_merge_patch_diff('{"a":1,"b":2,"c":3}', '{"a":1,"b":99,"d":4}');
SELECT json_deep_merge('{"a":1,"b":2}', '{"b":null}');
SELECT json_strip_nulls('{"a":1,"b":null,"c":null,"d":2}');
```
