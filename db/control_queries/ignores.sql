-- name: CreateIgnore :exec
INSERT INTO ignores (id, network_id, mask, created_at, level) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(network_id, mask) DO UPDATE SET level = excluded.level;

-- name: DeleteIgnore :exec
DELETE FROM ignores WHERE network_id = ? AND mask = ?;

-- name: ListIgnores :many
SELECT mask, level FROM ignores WHERE network_id = ? ORDER BY created_at;
