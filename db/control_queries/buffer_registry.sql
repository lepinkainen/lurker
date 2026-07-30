-- name: LookupBufferRegistry :one
SELECT network_id, name, kind FROM buffer_registry WHERE id = ?;

-- name: LookupBufferRegistryByName :one
SELECT id, kind, created_at, sort_order FROM buffer_registry WHERE network_id = ? AND name = ?;

-- name: InsertBufferRegistry :exec
INSERT INTO buffer_registry(id, network_id, name, kind, created_at, sort_order) VALUES (?, ?, ?, ?, ?, ?);

-- name: NextChannelSortOrder :one
-- 0 while the network's channel order is untouched (pure-alphabetical
-- default); MAX+1 once any channel has a manual position, so new channels
-- append to the end of the user's order instead of jumping to the top.
SELECT CAST(
  CASE WHEN COALESCE(MAX(sort_order), 0) > 0 THEN MAX(sort_order) + 1 ELSE 0 END
  AS INTEGER
) FROM buffer_registry WHERE network_id = ? AND kind = 'channel';

-- name: ListBufferRegistryForNetwork :many
SELECT id, name, kind, created_at, sort_order FROM buffer_registry WHERE network_id = ? ORDER BY id;

-- name: DeleteBufferRegistry :exec
DELETE FROM buffer_registry WHERE id = ?;

-- name: SetBufferSortOrder :exec
UPDATE buffer_registry SET sort_order = ? WHERE id = ?;

-- name: ListChannelBuffersForNetwork :many
-- Ordered the way clients render channels: (sort_order, case-insensitive name).
SELECT id, sort_order FROM buffer_registry WHERE network_id = ? AND kind = 'channel' ORDER BY sort_order, lower(name);
