CREATE TABLE highlights (
  id         BLOB PRIMARY KEY CHECK (length(id) = 16),
  pattern    TEXT NOT NULL UNIQUE COLLATE NOCASE,
  created_at TEXT NOT NULL
);
