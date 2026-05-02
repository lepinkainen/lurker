CREATE TABLE buffers (
  id           BLOB    PRIMARY KEY CHECK (length(id) = 16),
  name         TEXT    NOT NULL UNIQUE,
  kind         TEXT    NOT NULL CHECK (kind IN ('channel','query','status')),
  topic        TEXT,
  joined       INTEGER NOT NULL DEFAULT 0,
  last_seen_id BLOB,
  created_at   TEXT    NOT NULL
);

CREATE TABLE messages (
  rowid      INTEGER PRIMARY KEY AUTOINCREMENT,
  id         BLOB    NOT NULL UNIQUE CHECK (length(id) = 16),
  buffer_id  BLOB    NOT NULL REFERENCES buffers(id) ON DELETE CASCADE,
  msgid      TEXT,
  ts         TEXT    NOT NULL,
  sender     TEXT    NOT NULL,
  account    TEXT,
  kind       TEXT    NOT NULL,
  target     TEXT,
  content    TEXT    NOT NULL DEFAULT '',
  raw        TEXT    NOT NULL
);

CREATE INDEX messages_buffer_id ON messages(buffer_id, id);
CREATE INDEX messages_buffer_ts ON messages(buffer_id, ts);
CREATE UNIQUE INDEX messages_msgid
  ON messages(msgid) WHERE msgid IS NOT NULL;

CREATE VIRTUAL TABLE messages_fts USING fts5(
  content, sender,
  content='messages', content_rowid='rowid',
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, content, sender)
    VALUES (new.rowid, new.content, new.sender);
END;

CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content, sender)
    VALUES('delete', old.rowid, old.content, old.sender);
END;

CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content, sender)
    VALUES('delete', old.rowid, old.content, old.sender);
  INSERT INTO messages_fts(rowid, content, sender)
    VALUES (new.rowid, new.content, new.sender);
END;
