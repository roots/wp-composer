-- +goose Up
ALTER TABLE packages ADD COLUMN trunk_revision INTEGER;

-- +goose Down
ALTER TABLE packages DROP COLUMN trunk_revision;
