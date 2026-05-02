CREATE TABLE ignores (
  id         BLOB    PRIMARY KEY CHECK (length(id) = 16),
  network_id BLOB    NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
  mask       TEXT    NOT NULL,
  created_at TEXT    NOT NULL,
  UNIQUE (network_id, mask)
);
