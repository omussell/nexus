# VULCANUS

Vulcanus ingests data from Nauvis and Fulgora into a DuckDB database, enabling citation and retraction analysis.

## Overview

Vulcanus is a batch worker that connects to RabbitMQ, consumes CROID events from Nauvis and Fulgora, and ingests each record into DuckDB. It runs as a container that processes messages until stopped with SIGINT or SIGTERM.

## Key Functionalities

* **RabbitMQ Consumer:** Connects to the CROID exchange, consumes events one at a time (QoS=1), and exits on shutdown signal.
* **Per-Record Ingestion:** Each CROID event triggers ingestion of a single record into the appropriate DuckDB table (`nauvis` or `<source>` for Fulgora sources).
* **Provider Isolation:** Nauvis DOI records land in a single `nauvis` table; each Fulgora source (e.g. `ror`, `retractionwatch`) gets its own table.
* **Retraction Mapping:** Derives a `retractions` view that pairs each Nauvis record with its `retractionwatch` notice on the paper's DOI.
* **Bidirectional Retraction Provenance:** Derives both passive (`X is-retracted-by Y`) and active (`Y retracts X`) provenance strings.
* **Faithful Preservation:** Ingests raw JSON records verbatim into a `JSON` column in DuckDB.
* **ROR Matching:** Optional `-match-ror` flag runs ROR entity matching against nauvis records after consuming.

## Running Vulcanus

```bash
go run . \
  [-rabbitmq amqp://localhost:5672] \
  [-exchange croid] \
  [-queue vulcanus] \
  [-routing-key ""] \
  [-duckdb vulcanus.duckdb] \
  [-match-ror]
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
go run . -duckdb vulcanus.duckdb -match-ror
vulcanus: starting consumer
vulcanus: waiting for messages (send SIGINT/SIGTERM to stop)
^C
vulcanus: shutting down...
vulcanus: ROR matching complete
vulcanus: done, DuckDB=vulcanus.duckdb
```

## How it works

See [How Vulcanus Works](how-it-works.md) for a detailed explanation of the consumer flow, DuckDB tables, and views.

## Deployment

Vulcanus is designed for container deployment:

1. Container starts → connects to RabbitMQ → begins consuming
2. If queue is empty: blocks waiting for messages
3. On SIGINT/SIGTERM: finishes current message, closes connection, exits
4. Max 30s shutdown timeout prevents hanging

Set `terminationGracePeriodSeconds: 35` in Kubernetes to allow graceful shutdown.

Multiple containers can consume from the same queue; RabbitMQ distributes messages across them.
