-- +goose Up
ALTER TABLE builds ADD COLUMN error_message TEXT;

-- +goose Down
ALTER TABLE builds DROP COLUMN error_message;
