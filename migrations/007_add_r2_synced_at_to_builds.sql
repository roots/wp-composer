-- +goose Up
ALTER TABLE builds ADD COLUMN r2_synced_at TEXT;

-- +goose Down
ALTER TABLE builds DROP COLUMN r2_synced_at;
