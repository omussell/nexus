# Nauvis HTTP Server

Nauvis can expose the DOI lookup as an HTTP service, so callers can map a DOI
to the input file that recorded it without spawning the `-query` CLI for each
request.

## Running it

```
./nauvis -serve [-host :8080] [-db ./nauvis.sqlite3]
```

- `-serve` turns on the HTTP handler (and implies `-db`).
- `-host` (default `localhost:8080`) is the address the listeners on.
- `-db` and `-in/-out/-jobs` are ignored in this mode.

It logs `nauvis: listening on <host> (...)` on start. Stop with `Ctrl-C`.

## Endpoint

```
GET /query?doi=<DOI>
```

The response is a JSON object with either a `file` or an `error` field:

Recorded:

```json
{"doi":"10.1001/jama.2023.24766","file":"9.json"}
```

Not recorded (HTTP 404):

```json
{"doi":"10.1/never","error":"no such item"}
```

### Responses and status codes

| Case                                             | Status | Body                                                             |
|--------------------------------------------------|--------|------------------------------------------------------------------|
| DOI present in the index                         | `200`  | `{"doi":"<DOI>","file":"<file>"}`                                |
| DOI not recorded                                 | `404`  | `{"doi":"<DOI>","error":"no such item"}`                        |
| Missing `doi` parameter                          | `400`  | `missing 'doi' query parameter`                                  |
| More than one `doi` parameter (e.g. `doi=a&doi=b`) | `400`  | `expected a single 'doi' query parameter`                       |
| Non-`GET` HTTP method                            | `405`  | `method not allowed`, `Allow: GET`                               |
| Database failure during lookup (rare)         | `500`  | `{"doi":"<DOI>","error":"<err>"}`, logged at `ERROR`             |

A missing DOI is *not* a server error — it is a normal `404`. Only unexpected
SQLite errors produce a `500`.

## Implementation

The handler lives in `internal/server`. It wraps a `*store.Store` and exposes a
single `GET /query` route through `net/http`:

```go
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	dois := r.URL.Query()["doi"]
	// ...single 'doi' param checks...
	item, err := s.st.GetByDOI(r.Context(), doi)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// 404
	case err != nil:
		// 500 + log
	default:
		// 200 {"doi":"<DOI>","file":"<file>"}
	}
}
```

## Caveats

- **Read-only, no auth.** The server offers an unauthenticated read-only view on
  the local index. Run it only where that's acceptable, or behind a network or
  reverse proxy.
- **Read concurrency.** `store.Open` opens SQLite in WAL mode, so the server can
  serve reads without contending with (in-process) writers. Nauvis does not
  currently expose a process-mode server, so in practice there is no writer
  contending with the server.
