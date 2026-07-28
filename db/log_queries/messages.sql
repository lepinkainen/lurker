-- name: InsertLogMessage :execrows
INSERT OR IGNORE INTO messages
  (id, buffer_id, msgid, ts, sender, userhost, account, kind, target, content, raw)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: LogMessageInBuffer :one
SELECT EXISTS(SELECT 1 FROM messages WHERE buffer_id = ? AND id = ?);

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
