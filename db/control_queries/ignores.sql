-- name: CreateIgnore :exec
INSERT OR IGNORE INTO ignores (id, network_id, mask, created_at) VALUES (?, ?, ?, ?);

-- name: DeleteIgnore :exec
DELETE FROM ignores WHERE network_id = ? AND mask = ?;

-- name: ListIgnores :many
SELECT mask FROM ignores WHERE network_id = ? ORDER BY created_at;
