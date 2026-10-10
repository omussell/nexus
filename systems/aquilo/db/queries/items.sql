-- name: CountItems :one
SELECT COUNT(*) FROM items;

-- name: InsertItem :exec
INSERT INTO items (croid, record_json)
VALUES (?, ?);

-- name: GetItemByCroid :one
SELECT croid, record_json
FROM items
WHERE croid = ?
LIMIT 1;

-- name: ListItemsCroids :many
SELECT croid FROM items ORDER BY croid
LIMIT ? OFFSET ?;

-- name: QueryItems :many
SELECT croid, record_json FROM items ORDER BY croid
LIMIT ? OFFSET ?;
