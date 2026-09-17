# CROID

The CROID (Crossref Research Object ID) system provides unique, URL-safe identifiers for research objects to track metadata across different systems.

## Overview

A CROID acts as an identifier separate from others like DOIs or ORCIDs, allowing tracking of research objects regardless of metadata changes or missing standards.

## API Reference

### Create CROID
**POST** `/croid`

Links an object's identity (via `cro_type`, `cro_value`, `system`) to a CROID.

**Request Body:**
```json
{
  "cro_type": "DOI",
  "cro_value": "10.5555/12345678",
  "system": "nauvis"
}
```

**Responses:**
- `201 Created`: New CROID minted.
- `200 OK`: Existing CROID found (Idempotent).
- `400 Bad Request`: Missing or invalid parameters.

### Retrieve Metadata
**GET** `/croid/{croid}`

Fetches the metadata associated with a specific CROID.

**Response Body:**
```json
{
  "cro_type": "DOI",
  "cro_value": "10.5555/12345678",
  "system": "nauvis",
  "croid": "aaaaa12345_aaaaa12345-aaaaa12345",
  "created_at": "2026-05-28T19:04:17Z"
}
```

**Responses:**
- `200 OK`: Success.
- `404 Not Found`: CROID does not exist.

## Implementation Details

- **Language:** Go 1.25
- **Database:** SQLite (using `modernc.org/sqlite`: a pure-Go, CGO-free driver).
- **Querying:** `sqlc` generates type-safe database access logic.
- **Concurrency:** The service is idempotent; concurrent creation requests for the same identity safely return the single existing CROID.

## Quick Start
To run the service:
```bash
go run main.go --addr :8080 --db croid.sqlite3
```

To test via HTTP:
```bash
curl -X POST -H "Content-Type: application/json" -d '{"cro_type":"DOI","cro_value":"10.5555/example","system":"nauvis"}' http://localhost:8080/croid
```

### CLI: Generate a CROID from the command line
The CLI can mint a CROID without starting the HTTP server, using the `--generate` flag. It runs the exact same code path as `POST /croid` (same SQLite setup, schema, and `store.Create`), so it mints, dedupes, and validates identically:

```bash
go run . --generate --db croid.sqlite3
```

With a JSON identity input, the CLI resolves or mints a CROID for that identity (deduplicating against whatever already lives in `--db`):

```bash
go run . --generate --db croid.sqlite3 --input '{"cro_type":"DOI","cro_value":"10.5555/example","system":"nauvis"}'
```

The output matches the `POST /croid` response body:

```json
{
  "cro_type": "DOI",
  "cro_value": "10.5555/example",
  "system": "nauvis",
  "croid": "aaaaa12345_aaaaa12345-aaaaa12345",
  "created_at": "2026-09-17T20:11:10Z"
}
```

Re-running the command for the same identity returns the same CROID (`"created_at"` unchanged) rather than minting a new one. If `--input` omits a required field (`cro_type`, `cro_value`, or `system`) the command exits non-zero with an error, mirroring the `400 Bad Request` the HTTP endpoint returns.