-- name: GetURLPreview :one
SELECT url, kind, COALESCE(title,'') AS title, COALESCE(description,'') AS description,
       COALESCE(image_url,'') AS image_url, COALESCE(site_name,'') AS site_name,
       COALESCE(width,0) AS width, COALESCE(height,0) AS height, COALESCE(mime,'') AS mime,
       fetched_at, COALESCE(error,'') AS error
FROM url_previews WHERE url = ?;

-- name: UpsertURLPreview :exec
INSERT INTO url_previews(url, kind, title, description, image_url, site_name,
                         width, height, mime, fetched_at, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(url) DO UPDATE SET
  kind=excluded.kind,
  title=excluded.title,
  description=excluded.description,
  image_url=excluded.image_url,
  site_name=excluded.site_name,
  width=excluded.width,
  height=excluded.height,
  mime=excluded.mime,
  fetched_at=excluded.fetched_at,
  error=excluded.error;

-- name: DeleteURLPreviewsBefore :execrows
DELETE FROM url_previews WHERE fetched_at < ?;
