CREATE TABLE networks (
  id          INTEGER PRIMARY KEY,
  name        TEXT    NOT NULL,
  name_ci     TEXT    NOT NULL UNIQUE,
  host        TEXT    NOT NULL,
  port        INTEGER NOT NULL,
  tls         INTEGER NOT NULL DEFAULT 1,
  nick        TEXT    NOT NULL,
  realname    TEXT,
  sasl_user   TEXT,
  sasl_pass   TEXT,
  autoconnect INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT    NOT NULL
);

CREATE TABLE buffer_registry (
  id         INTEGER PRIMARY KEY,
  network_id INTEGER NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
  name       TEXT    NOT NULL,
  kind       TEXT    NOT NULL CHECK (kind IN ('channel','query','status')),
  created_at TEXT    NOT NULL,
  UNIQUE (network_id, name)
);
