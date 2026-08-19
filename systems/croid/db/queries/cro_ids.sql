-- name: InsertCroid :exec
INSERT INTO cro_ids (croid, cro_type, cro_value, system, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetCroidByCroid :one
SELECT id, croid, cro_type, cro_value, system, created_at
FROM cro_ids
WHERE croid = ?
LIMIT 1;

-- name: GetCroidByIdentity :one
SELECT id, croid, cro_type, cro_value, system, created_at
FROM cro_ids
WHERE cro_type = ? AND cro_value = ? AND system = ?
LIMIT 1;
