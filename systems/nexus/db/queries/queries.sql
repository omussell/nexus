-- name: InsertRelationship :execresult
INSERT INTO relationships (source, target, relationship, evidence, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetRelationshipByTarget :one
SELECT id, source, target, relationship, evidence, created_at
FROM relationships
WHERE target = ?
LIMIT 1;

-- name: ListBySource :many
SELECT id, source, target, relationship, evidence, created_at
FROM relationships
WHERE source = ?
ORDER BY id;

-- name: ListByRelationship :many
SELECT id, source, target, relationship, evidence, created_at
FROM relationships
WHERE relationship = ?
ORDER BY id;

-- name: ListAll :many
SELECT id, source, target, relationship, evidence, created_at
FROM relationships
ORDER BY id;
