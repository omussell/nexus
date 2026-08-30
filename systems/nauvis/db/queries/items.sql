-- name: InsertItem :exec
INSERT INTO nauvis (file, doi)
VALUES (?, ?);

-- name: GetItemByDOI :one
SELECT id, file, doi
FROM nauvis
WHERE doi = ?
LIMIT 1;
