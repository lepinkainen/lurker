-- name: ListBufferSettings :many
SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at
FROM buffer_settings;

-- name: GetBufferSettings :one
SELECT buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at
FROM buffer_settings WHERE buffer_id = ?;

-- name: BufferRegistryExists :one
SELECT COUNT(1) FROM buffer_registry WHERE id = ?;

-- name: GetBufferRegistryKind :one
SELECT kind FROM buffer_registry WHERE id = ?;

-- name: UpsertBufferSettings :exec
INSERT INTO buffer_settings(buffer_id, show_embeds, show_presence_events, collapse_presence_events, pinned, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(buffer_id) DO UPDATE SET
  show_embeds=excluded.show_embeds,
  show_presence_events=excluded.show_presence_events,
  collapse_presence_events=excluded.collapse_presence_events,
  pinned=excluded.pinned,
  updated_at=excluded.updated_at;
