-- name: InsertHighlight :exec
INSERT OR IGNORE INTO highlights (id, pattern, created_at) VALUES (?, ?, ?);

-- name: DeleteAllHighlights :exec
DELETE FROM highlights;

-- name: ListHighlights :many
SELECT pattern FROM highlights ORDER BY created_at;
