-- Replaces network-wide msgid uniqueness with per-buffer uniqueness so a
-- single Bluesky post can land in multiple buffers (e.g. #home and a future
-- #feed:*). One-time cost: the CREATE rebuilds the index for every existing
-- message row -- on large per-network log DBs this can stall startup for
-- several seconds.
DROP INDEX IF EXISTS messages_msgid;
CREATE UNIQUE INDEX messages_buffer_msgid
  ON messages(buffer_id, msgid) WHERE msgid IS NOT NULL;
