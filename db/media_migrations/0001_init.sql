CREATE TABLE media (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, mime TEXT NOT NULL,
  source_hash TEXT NOT NULL UNIQUE, variants TEXT NOT NULL,
  size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX idx_media_created_at ON media(created_at DESC);
