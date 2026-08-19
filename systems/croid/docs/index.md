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
To test:
```bash
curl -X POST -H "Content-Type: application/json" -d '{"cro_type":"DOI","cro_value":"10.5555/example","system":"nauvis"}' http://localhost:8080/croid
```