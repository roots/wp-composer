-- +goose Up
ALTER TABLE packages ADD COLUMN wporg_version TEXT;

-- +goose Down
ALTER TABLE packages DROP COLUMN wporg_version;
