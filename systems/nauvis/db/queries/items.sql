-- name: InsertItem :exec
INSERT INTO nauvis (file, doi, croid)
VALUES (?, ?, ?);

-- name: GetItemByDOI :one
SELECT id, file, doi, croid
FROM nauvis
WHERE doi = ?
LIMIT 1;

-- name: UpdateItemCroid :exec
UPDATE nauvis SET croid = ? WHERE doi = ?;

-- name: GetDOIsWithCroids :many
SELECT doi, croid FROM nauvis WHERE croid IS NOT NULL AND croid != '' ORDER BY doi;
