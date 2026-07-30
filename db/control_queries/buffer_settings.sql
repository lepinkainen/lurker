-- name: ListBufferSettings :many
SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at, archived, pin_order
FROM buffer_settings;

-- name: GetBufferSettings :one
SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at, archived, pin_order
FROM buffer_settings WHERE buffer_id = ?;

-- name: BufferRegistryExists :one
SELECT COUNT(1) FROM buffer_registry WHERE id = ?;

-- name: GetBufferRegistryKind :one
SELECT kind FROM buffer_registry WHERE id = ?;

-- name: UpsertBufferSettings :exec
INSERT INTO buffer_settings(buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at, archived, pin_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(buffer_id) DO UPDATE SET
  show_embeds=excluded.show_embeds,
  show_presence_events=excluded.show_presence_events,
  collapse_presence_events=excluded.collapse_presence_events,
  pinned=excluded.pinned,
  updated_at=excluded.updated_at,
  archived=excluded.archived,
  pin_order=excluded.pin_order;

-- name: MaxPinOrder :one
SELECT CAST(COALESCE(MAX(pin_order), -1) AS INTEGER) FROM buffer_settings WHERE pinned = 1;

-- name: SetBufferPinOrder :exec
UPDATE buffer_settings SET pin_order = ? WHERE buffer_id = ?;

-- name: ListPinnedBuffers :many
-- Ordered the way clients render the pinned section: (pin_order, name).
SELECT s.buffer_id, s.pin_order FROM buffer_settings s
JOIN buffer_registry r ON r.id = s.buffer_id
WHERE s.pinned = 1 ORDER BY s.pin_order, lower(r.name);
