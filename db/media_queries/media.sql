-- name: GetMediaBySourceHash :one
SELECT id, kind, mime, source_hash, variants, size, width, height, created_at, updated_at
FROM media WHERE source_hash = ?;

-- name: GetMedia :one
SELECT id, kind, mime, source_hash, variants, size, width, height, created_at, updated_at
FROM media WHERE id = ?;

-- name: InsertMedia :exec
INSERT INTO media(id, kind, mime, source_hash, variants, size, width, height, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteMedia :execrows
DELETE FROM media WHERE id = ?;
