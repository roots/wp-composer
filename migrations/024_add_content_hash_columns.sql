-- +goose Up
ALTER TABLE packages ADD COLUMN content_hash TEXT;
ALTER TABLE packages ADD COLUMN deployed_hash TEXT;
ALTER TABLE packages ADD COLUMN content_changed_at TEXT;

-- +goose Down
ALTER TABLE packages DROP COLUMN content_changed_at;
ALTER TABLE packages DROP COLUMN deployed_hash;
ALTER TABLE packages DROP COLUMN content_hash;
