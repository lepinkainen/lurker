-- name: ListLogBuffers :many
SELECT id, name, kind, COALESCE(topic,'') AS topic, last_seen_id, created_at
FROM buffers ORDER BY id;

-- name: LookupLogBuffer :one
SELECT name, kind FROM buffers WHERE id = ?;

-- name: UpdateLogBufferTopic :exec
UPDATE buffers SET topic = ? WHERE name = ?;

-- name: UpdateLogBufferLastSeen :exec
UPDATE buffers SET last_seen_id = ? WHERE name = ?;

-- name: LookupLogBufferByName :one
SELECT id, COALESCE(topic,'') AS topic, last_seen_id, created_at FROM buffers WHERE name = ?;

-- name: InsertLogBuffer :exec
INSERT INTO buffers(id, name, kind, created_at) VALUES (?, ?, ?, ?);

-- name: GetLogBufferTopicLastSeen :one
SELECT COALESCE(topic,'') AS topic, last_seen_id FROM buffers WHERE name = ?;
