-- +goose Up
ALTER TABLE builds ADD COLUMN pid INTEGER;

-- +goose Down
-- SQLite does not support DROP COLUMN before 3.35.0; recreate if needed.
