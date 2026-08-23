-- name: InsertDataset :execresult
INSERT INTO datasets (source, version, collected_at, initial_input, output)
VALUES (?, ?, ?, ?, ?);

-- name: GetDataset :one
SELECT id, source, version, collected_at, initial_input, output
FROM datasets
WHERE source = ? AND version = ?
LIMIT 1;

-- name: ListDatasetsBySource :many
SELECT id, source, version, collected_at, initial_input, output
FROM datasets
WHERE source = ?
ORDER BY collected_at DESC;

-- name: UpsertLatest :exec
INSERT INTO latest (source, version, dataset_id)
VALUES (?, ?, ?)
ON CONFLICT (source) DO UPDATE SET
    version    = excluded.version,
    dataset_id = excluded.dataset_id;

-- name: GetLatest :one
SELECT l.source AS source, l.version AS version, d.collected_at AS collected_at, d.initial_input AS initial_input, d.output AS output
FROM latest l
JOIN datasets d ON d.id = l.dataset_id
WHERE l.source = ?
LIMIT 1;

-- name: ListLatest :many
SELECT l.source AS source, l.version AS version, d.collected_at AS collected_at, d.initial_input AS initial_input, d.output AS output
FROM latest l
JOIN datasets d ON d.id = l.dataset_id
ORDER BY l.source;
