-- name: InsertMessagePreviewLink :exec
INSERT OR IGNORE INTO message_previews(message_id, url, position)
VALUES (?, ?, ?);
