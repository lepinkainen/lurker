-- name: LookupBufferRegistry :one
SELECT network_id, name, kind FROM buffer_registry WHERE id = ?;

-- name: LookupBufferRegistryByName :one
SELECT id, kind, created_at FROM buffer_registry WHERE network_id = ? AND name = ?;

-- name: InsertBufferRegistry :exec
INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?);

-- name: ListBufferRegistryForNetwork :many
SELECT id, name, kind, created_at, sort_order FROM buffer_registry WHERE network_id = ? ORDER BY id;

-- name: DeleteBufferRegistry :exec
DELETE FROM buffer_registry WHERE id = ?;

-- name: SetBufferSortOrder :exec
UPDATE buffer_registry SET sort_order = ? WHERE id = ?;

-- name: ListChannelBuffersForNetwork :many
SELECT id, sort_order FROM buffer_registry WHERE network_id = ? AND kind = 'channel' ORDER BY id;
