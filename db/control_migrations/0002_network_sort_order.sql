ALTER TABLE networks ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

UPDATE networks
SET sort_order = id;
