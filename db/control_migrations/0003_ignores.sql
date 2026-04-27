CREATE TABLE ignores (
  id         INTEGER PRIMARY KEY,
  network_id INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
  mask       TEXT    NOT NULL,
  created_at TEXT    NOT NULL,
  UNIQUE (network_id, mask)
);
