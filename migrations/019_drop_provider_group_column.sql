-- +goose Up
ALTER TABLE packages DROP COLUMN provider_group;

-- +goose Down
ALTER TABLE packages ADD COLUMN provider_group TEXT;
