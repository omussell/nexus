-- name: InsertItem :exec
INSERT INTO items (file, doi)
VALUES (?, ?);

-- name: GetItemByDOI :one
SELECT id, file, doi
FROM items
WHERE doi = ?
LIMIT 1;
