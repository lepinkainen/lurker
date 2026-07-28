-- autoconnect was written as a literal 1 on insert and never read anywhere.
-- Connection policy is decided by config.yaml at boot (source of truth) and
-- the disabled flag at runtime.
ALTER TABLE networks DROP COLUMN autoconnect;
