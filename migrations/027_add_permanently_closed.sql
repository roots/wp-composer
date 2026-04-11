-- +goose Up
ALTER TABLE packages ADD COLUMN permanently_closed INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE packages DROP COLUMN permanently_closed;
