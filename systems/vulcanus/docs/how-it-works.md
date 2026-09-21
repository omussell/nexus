# How Vulcanus Works

This page explains how Vulcanus operates, from RabbitMQ CROID events to the
DuckDB database it maintains. It is a companion to the [Overview](index.md);
see there for what Vulcanus is and what it aims to.

## At a glance

Vulcanus is a RabbitMQ consumer. Nauvis and Fulgora each publish CROID events to
the `croid` exchange whenever they mint a new CROID. Vulcanus listens on a
queue bound to that exchange, and for every message it:

1. loads the full CROID record (`record` field) from the RabbitMQ message body,
2. routes the record into the appropriate DuckDB table, and
3. continues processing the next message.

```
Nauvis ──mint──▶  RabbitMQ `croid` exchange  ──consume──▶  DuckDB `nauvis` table
Fulgora ──mint──▶  RabbitMQ `croid` exchange  ──consume──▶  DuckDB `<source>` tables
```

The two providers stay separate: Nauvis lands in a single `nauvis` table, while
each Fulgora source (e.g. `ror`, `retractionwatch`) gets its own table. They can
arrive at different times without clobbering each other.

After the source tables exist, Vulcanus also builds a thin **derived view**
`retractions` that pairs each Nauvis record with its matching
`retractionwatch` notice on the paper's DOI (`nauvis.DOI` =
`retractionwatch.OriginalPaperDOI`), exposing both full records plus the
matching DOI. The view is rebuilt idempotently on every ingest and is a
projection over the loaded tables — it never copies data.

This is not transformation: Vulcanus never parses or normalises the payload. It
only preserves it, one record at a time, in a single column of JSON text. Every
record a provider saw is ingested; nothing is filtered away.

## The stages

The pipeline is split into two packages and one entry point, each with one job.

### 1. RabbitMQ consumer — `internal/rabbitmq`

`rabbitmq.New(url, exchange, queue, routingKey, handler, logger)` creates a
persistent AMQP connection, declares the queue, binds it to the exchange, sets
QoS to 1 (one message at a time), and starts consuming.

The handler receives each `Record` with fields:
- `CROID` — the URL-safe 32-character CROID string
- `CroType` — the identity type (e.g. `DOI`)
- `CroValue` — the identity value (e.g. `10.1/paper`)
- `System` — the system that minted it (`nauvis` or `fulgora:<source>`)
- `Record` — the full JSON record, preserved as-is

```go
type Record struct {
    CROID   string
    CroType string
    CroValue string
    System  string
    Record  []byte
}
```

The consumer exposes a `Done() <-chan struct{}` that is closed when the consumer
loop exits (connection loss, explicit close, etc.), allowing the main process to
detect lifecycle events.

`Close()` shuts down the consumer gracefully, waiting up to 30 seconds for the
connection to close cleanly.

### 2. Ingest — `internal/ingest`

`OpenDB(path)` opens the DuckDB database with write-ahead-log mode enabled and
returns a `*sql.DB` for use by the ingest writer.

The `ingestWriter` type handles routing CROID records into the correct DuckDB
table. It maintains a cache of prepared `INSERT` statements keyed by table name:

```go
func (w *ingestWriter) Ingest(ctx context.Context, rec rabbitmq.Record) error {
    table := tableName(rec.System)  // "nauvis" or "<source>"
    stmt := w.ensureStmt(table)     // prepare INSERT INTO "table" (record) VALUES (?)
    stmt.Exec(rec.Record)           // bind and execute
}
```

The implementation:
- **Prepared statements** — each table's `INSERT` is prepared once and reused,
  so the bind cost is minimal.
- **Safe table names** — system and source names that become table identifiers
  are wrapped in double quotes with any embedded quotes doubled, so registry
  values can't break (or inject into) the DDL.
- **Faithful preservation** — the bytes of each record are bound verbatim into
  the `JSON` column. No round-tripping through a struct, no re-serialising, no
  field selection: whatever a provider emitted is exactly what lands in DuckDB.
- **One message at a time** — QoS=1 ensures the next message is only dispatched
  after the previous one is successfully ingested, providing backpressure.

### 3. Entry point — `main.go`

`main.go` runs as a batch worker: it connects to RabbitMQ, begins consuming
messages, and blocks until a shutdown signal arrives.

```
-rabbitmq    RabbitMQ AMQP URL (default amqp://localhost:5672)
-exchange    RabbitMQ exchange to consume from (default croid)
-queue       RabbitMQ queue name to declare/bind (default vulcanus)
-routing-key RabbitMQ routing key (empty = all)
-duckdb      the DuckDB file to write (default vulcanus.duckdb)
-match-ror   run ROR matching against nauvis records after consuming
```

Flow:
1. Open DuckDB once via `ingest.OpenDB()`.
2. Create the `ingestWriter` and wire it as the RabbitMQ message handler.
3. Start the consumer — it begins processing messages immediately.
4. Block on either `sigCh` (SIGINT/SIGTERM) or `consumer.Done()`.
5. On signal: initiate graceful shutdown with a 30s timeout.
6. If `-match-ror` is set: run ROR entity matching against nauvis records.

The two providers are independent — Nauvis and Fulgora events can arrive at
different times, and Vulcanus routes each to the correct table.

## The database

Every *table* has a single column and is loaded identically — one row per
CROID event, the raw JSON document preserved as-is:

| table | column   | type    | notes |
|-------|----------|---------|-------|
| `nauvis` (Nauvis) | `record` | JSON    | one row per CROID event; the raw document as Nauvis wrote it |
| `<source>` (Fulgora, one per source) | `record` | JSON    | e.g. `ror`, `retractionwatch`; same shape as `nauvis`, but each source has its own table |

In addition, the ingest derives two **views** that sit on the same Nauvis↔
retractionwatch `JOIN`. The `retractions` view pairs each Nauvis record with
its `retractionwatch` notice on the paper's DOI so a consumer can pull the full
pair in one query; the narrower `retraction_provenance` view reduces exactly
those matches to two consumer-ready provenance strings per match — one from
Nauvis's perspective (`X is-retracted-by Y`) and one from the retraction
notice's (`Y retracts X`). Both are derived objects — they are not tables that
get loaded, and never hold a copy of the bytes; they are rebuilt on `CREATE OR
REPLACE VIEW` every time a provider ingests, and only become queryable once
**both** source tables exist.

| view | columns | type | notes |
|------|---------|------|-------|
| `retractions` | `nauvis_record` | JSON | the full Nauvis record (Crossref item) |
| | `retractionwatch_record` | JSON | the full retractionwatch record |
| | `matched_doi` | VARCHAR | the DOI that linked the two (Nauvis's `DOI`) |
| `retraction_provenance` | `nauvis_record` | JSON | the Nauvis record for the passive row; swapped for the active row |
| | `retractionwatch_record` | JSON | the retractionwatch record for the passive row; swapped for the active row |
| | `paper_doi` | VARCHAR | Nauvis's `DOI` in the passive row; retraction notices' `RetractionDOI` in the active. Equals the join key in the passive row. |
| | `retraction_doi` | VARCHAR | Retraction notice's own DOI (`RetractionDOI`) in the passive row; Nauvis's `DOI` in the active. |
| | `provenance` | VARCHAR | bidirectional provenance: `X is-retracted-by Y` and `Y retracts X` per match, two rows per join result |

Nauvis and Fulgora are kept **separate**: Nauvis always lands in the single
`nauvis` table, while each Fulgora source gets its own table named after the
source. A row in any table is a full JSON object from that provider's output —
one record per input record. Because the column is declared `JSON`,
DuckDB's JSON functions (`JSON_SCHEMA`, `JSON_EXTRACT`, ...) are available
directly against the stored text once the load has happened.

Running the consumer over time accumulates records from both providers into the
same DuckDB database. Because there is no table clearing step, re-running a
provider simply adds more rows — the consumer is append-only.

## Running Vulcanus

```bash
go run . [-rabbitmq amqp://localhost:5672] [-duckdb vulcanus.duckdb] [-match-ror]
```

| flag | default | meaning |
|------|---------|---------|
| `-rabbitmq` | `amqp://localhost:5672` | RabbitMQ AMQP URL |
| `-exchange` | `croid` | RabbitMQ exchange to consume from |
| `-queue` | `vulcanus` | RabbitMQ queue name to declare/bind |
| `-routing-key` | (empty) | RabbitMQ routing key (empty = all) |
| `-duckdb` | `vulcanus.duckdb` | DuckDB database file to write |
| `-match-ror` | `false` | Run ROR matching after consuming |

### Example

```bash
$ go run . -duckdb vulcanus.duckdb -match-ror
vulcanus: starting consumer
vulcanus: waiting for messages (send SIGINT/SIGTERM to stop)
^C
vulcanus: shutting down...
vulcanus: ROR matching complete
vulcanus: done, DuckDB=vulcanus.duckdb
```

The resulting `vulcanus.duckdb` holds one table per source; list them with the
DuckDB CLI:

```
$ duckdb vulcanus.duckdb
duckdb> .tables
nauvis            retractionwatch  ror
```

The key contract is faithfulness: Vulcanus treats the bytes coming in each
RabbitMQ message as the data, and DuckDB as the place to store them for later
analysis.

The derived `retractions` view is the one exception to "preserve bytes, do not
transform": its purpose is to *link* two existing bytes columns by a key — the
Nauvis record's `DOI` and the `retractionwatch` record's `OriginalPaperDOI` —
and expose both alongside the key that tied them together. No record's content
is rewritten, re-serialised, or pruned; the view is a thin projection over the
two already-loaded tables:

```sql
CREATE OR REPLACE VIEW retractions AS
SELECT
    n.record AS nauvis_record,
    r.record AS retractionwatch_record,
    json_extract_string(n.record, '$.DOI') AS matched_doi
FROM nauvis AS n
JOIN retractionwatch AS r
  ON json_extract_string(n.record, '$.DOI') =
     json_extract_string(r.record, '$.OriginalPaperDOI')
```

Because it is a view (not a table), it is always in sync with whatever is
currently in the two source tables: as new messages arrive, the view's result
set updates automatically on the next query. The view is also idempotent across
consumption — the SQL uses `CREATE OR REPLACE`, so repeated ingests leave a
single, up-to-date view.

### `retraction_provenance` view

Each Nauvis↔retractionwatch match produces **two** rows — one passive and one
active — in a `UNION ALL`:

| direction | format | example |
|-----------|--------|---------|
| passive (existing) | `<DOI> is-retracted-by <RetractionDOI>` | `10.1/paper is-retracted-by 10.1/retract` |
| active | `<RetractionDOI> retracts <DOI>` | `10.1/retract retracts 10.1/paper` |

The passive row keeps its original column semantics (Nauvis columns stay in the
`nauvis_record` and `paper_doi` columns). In the active row, both the record
columns and column assignments are swapped so that `paper_doi` reflects the
retraction notice's DOI while `provenance` reads naturally from the retraction
notice's perspective. The `(paper_doi, retraction_doi)` pair is identical across
both directions.

Because the same join key drives both sides, the view is always in sync with the
`retractions` view — a consumer can query either to discover which pairs exist,
then use `provenance` for exports or joins without per-row string assembly.

## Deployment

Vulcanus is designed for container deployment. It runs continuously, consuming
messages until stopped:

1. Container starts → connects to RabbitMQ → begins consuming
2. If queue is empty: blocks waiting for messages
3. On SIGINT/SIGTERM: finishes current message, closes connection, exits
4. Max 30s shutdown timeout prevents hanging

Set `terminationGracePeriodSeconds: 35` in Kubernetes to allow graceful shutdown.

Multiple containers can consume from the same queue; RabbitMQ distributes
messages across them, enabling horizontal scaling.

## Design notes

- **RabbitMQ for delivery, DuckDB for analysis.** Nauvis and Fulgora publish to
  RabbitMQ; DuckDB is the column store that makes it cheap to run queries over
  the full records once they are on disk.
- **Round-trip preservation over transformation.** The ingest step preserves
  bytes, not parse results. There is no value in re-serialising JSON that is
  already compact.
- **Append-only ingestion.** Unlike the previous file-based ingest that cleared
  and replaced tables, the RabbitMQ consumer only adds rows. Re-running the
  consumer simply accumulates more records.
- **Separate tables per provider.** Nauvis is the "main" data in `nauvis`;
  Fulgora sources are enrichment data, each in its own table. Keeping them
  separate preserves that distinction and lets either arrive independently.
- **`main.go` is a batch worker.** It starts, consumes until stopped. All logic
  lives in `rabbitmq` and `ingest`; `main.go` only wires them together.
