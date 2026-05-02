CREATE TABLE network_connect_commands (
  network_id BLOB    NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
  position   INTEGER NOT NULL,
  command    TEXT    NOT NULL,
  PRIMARY KEY (network_id, position)
);
