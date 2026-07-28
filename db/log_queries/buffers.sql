-- name: ListLogBuffers :many
SELECT id, name, kind, COALESCE(topic,'') AS topic,
  COALESCE(topic_set_by,'') AS topic_set_by, COALESCE(topic_set_at,'') AS topic_set_at,
  last_seen_id, created_at
FROM buffers ORDER BY id;

-- name: LookupLogBuffer :one
SELECT name, kind FROM buffers WHERE id = ?;

-- name: UpdateLogBufferTopicState :exec
UPDATE buffers SET
  topic = COALESCE(sqlc.narg('topic'), topic),
  topic_set_by = COALESCE(sqlc.narg('topic_set_by'), topic_set_by),
  topic_set_at = COALESCE(sqlc.narg('topic_set_at'), topic_set_at)
WHERE name = sqlc.arg('name');

-- name: UpdateLogBufferLastSeen :exec
UPDATE buffers SET last_seen_id = ? WHERE name = ?;

-- name: LookupLogBufferByName :one
SELECT id, COALESCE(topic,'') AS topic, last_seen_id, created_at FROM buffers WHERE name = ?;

-- name: InsertLogBuffer :exec
INSERT INTO buffers(id, name, kind, created_at) VALUES (?, ?, ?, ?);

-- name: GetLogBufferTopicLastSeen :one
SELECT COALESCE(topic,'') AS topic, last_seen_id FROM buffers WHERE name = ?;
