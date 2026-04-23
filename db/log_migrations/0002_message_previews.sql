CREATE TABLE message_previews (
  message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  url        TEXT    NOT NULL,
  position   INTEGER NOT NULL,
  PRIMARY KEY (message_id, url)
);

CREATE INDEX idx_message_previews_msg ON message_previews(message_id);
