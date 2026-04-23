CREATE TABLE url_previews (
  url         TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  title       TEXT,
  description TEXT,
  image_url   TEXT,
  site_name   TEXT,
  width       INTEGER,
  height      INTEGER,
  mime        TEXT,
  fetched_at  TEXT NOT NULL,
  error       TEXT
);

CREATE INDEX idx_url_previews_fetched ON url_previews(fetched_at);
