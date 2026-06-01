-- name: LookupBufferRegistry :one
SELECT network_id, name, kind FROM buffer_registry WHERE id = ?;

-- name: LookupBufferRegistryByName :one
SELECT id, kind, created_at FROM buffer_registry WHERE network_id = ? AND name = ?;

-- name: InsertBufferRegistry :exec
INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?);

-- name: ListBufferRegistryForNetwork :many
SELECT id, name, kind, created_at FROM buffer_registry WHERE network_id = ? ORDER BY id;
