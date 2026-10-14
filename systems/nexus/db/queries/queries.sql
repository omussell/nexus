-- name: UpsertCroidInsert :exec
-- Attempt to insert; duplicate croids are silently ignored (unique constraint).
INSERT OR IGNORE INTO cro_ids (croid, system, created_at) VALUES (?, ?, ?);

-- name: GetCroidID :one
SELECT id FROM cro_ids WHERE croid = ?;

-- name: InsertRelationshipType :exec
INSERT OR IGNORE INTO relationship_types (relationship, created_at) VALUES (?, ?);

-- name: GetRelationshipTypeID :one
SELECT id FROM relationship_types WHERE relationship = ?;

-- name: InsertRelationship :exec
-- Callers should check existence first with RelationshipExists.
-- This uses INSERT OR IGNORE so a duplicate is silently skipped.
INSERT OR IGNORE INTO relationships (source_croid_id, target_croid_id, relationship_type_id, created_at)
VALUES (?, ?, ?, ?);

-- name: RelationshipExists :one
-- Returns id if the relationship already exists, 0 otherwise.
SELECT id FROM relationships
WHERE source_croid_id = ? AND target_croid_id = ? AND relationship_type_id = ?
LIMIT 1;

-- name: InsertEvidence :exec
INSERT OR IGNORE INTO relationship_evidence (relationship_id, evidence) VALUES (?, ?);

-- name: ListBySourceCroidID :many
SELECT r.id, r.source_croid_id, r.target_croid_id, r.relationship_type_id, r.created_at,
       e.evidence, sc.croid as source_croid, tc.croid as target_croid
FROM relationships r
LEFT JOIN relationship_evidence e ON e.relationship_id = r.id
LEFT JOIN cro_ids sc ON sc.id = r.source_croid_id
LEFT JOIN cro_ids tc ON tc.id = r.target_croid_id
WHERE r.source_croid_id = ?
ORDER BY r.id;

-- name: ListByRelationshipTypeID :many
SELECT r.id, r.source_croid_id, r.target_croid_id, r.relationship_type_id, r.created_at,
       e.evidence, sc.croid as source_croid, tc.croid as target_croid
FROM relationships r
LEFT JOIN relationship_evidence e ON e.relationship_id = r.id
LEFT JOIN cro_ids sc ON sc.id = r.source_croid_id
LEFT JOIN cro_ids tc ON tc.id = r.target_croid_id
WHERE r.relationship_type_id = ?
ORDER BY r.id;

-- name: ListAll :many
SELECT r.id, r.source_croid_id, r.target_croid_id, r.relationship_type_id, r.created_at,
       e.evidence, sc.croid as source_croid, tc.croid as target_croid
FROM relationships r
LEFT JOIN relationship_evidence e ON e.relationship_id = r.id
LEFT JOIN cro_ids sc ON sc.id = r.source_croid_id
LEFT JOIN cro_ids tc ON tc.id = r.target_croid_id
ORDER BY r.id;

-- name: GetRelationshipTypeIDs :many
SELECT id, relationship FROM relationship_types ORDER BY id;

-- name: GetCroidByID :one
SELECT id, croid, system, created_at FROM cro_ids WHERE id = ?;

-- name: GetCroidByCroid :one
SELECT id, croid, system, created_at FROM cro_ids WHERE croid = ?;

-- name: ListByTargetCroidID :many
SELECT r.id, r.source_croid_id, r.target_croid_id, r.relationship_type_id, r.created_at,
       e.evidence, sc.croid as source_croid, tc.croid as target_croid
FROM relationships r
LEFT JOIN relationship_evidence e ON e.relationship_id = r.id
LEFT JOIN cro_ids sc ON sc.id = r.source_croid_id
LEFT JOIN cro_ids tc ON tc.id = r.target_croid_id
WHERE r.target_croid_id = ?
ORDER BY r.id;
