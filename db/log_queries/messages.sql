-- name: InsertLogMessage :execrows
INSERT OR IGNORE INTO messages
  (id, buffer_id, msgid, ts, sender, userhost, account, kind, target, content, raw)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: LogMessageInBuffer :one
SELECT EXISTS(SELECT 1 FROM messages WHERE buffer_id = ? AND id = ?);

-- name: LatestLogMessageTS :one
-- Newest stored timestamp for a buffer (uses messages_buffer_ts index).
-- Empty string when the buffer has no messages.
SELECT CAST(COALESCE(MAX(ts), '') AS TEXT) AS ts FROM messages WHERE buffer_id = ?;

-- name: RecentLogMessages :many
SELECT id, buffer_id, COALESCE(msgid,'') AS msgid, ts, sender, COALESCE(userhost,'') AS userhost,
       COALESCE(account,'') AS account, kind, COALESCE(target,'') AS target, content
FROM (
  SELECT * FROM messages WHERE buffer_id = ? ORDER BY id DESC LIMIT ?
) ORDER BY id ASC;

-- name: LogMessagesBefore :many
SELECT id, buffer_id, COALESCE(msgid,'') AS msgid, ts, sender, COALESCE(userhost,'') AS userhost,
       COALESCE(account,'') AS account, kind, COALESCE(target,'') AS target, content
FROM (
  SELECT * FROM messages WHERE buffer_id = ? AND id < ?
  ORDER BY id DESC LIMIT ?
) ORDER BY id ASC;

-- name: DeleteLogMessagesForBuffer :exec
-- Explicit delete (not FK cascade) so the messages_ad trigger fires and
-- keeps the external-content FTS index in sync.
DELETE FROM messages WHERE buffer_id = ?;
