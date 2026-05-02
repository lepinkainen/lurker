CREATE TABLE buffer_settings (
  buffer_id BLOB PRIMARY KEY REFERENCES buffer_registry(id) ON DELETE CASCADE,
  show_embeds INTEGER NOT NULL DEFAULT 1,
  show_presence_events INTEGER NOT NULL DEFAULT 1,
  collapse_presence_events INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
