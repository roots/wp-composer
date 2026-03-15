-- +goose Up
ALTER TABLE builds ADD COLUMN discover_seconds INTEGER;
ALTER TABLE builds ADD COLUMN update_seconds INTEGER;
ALTER TABLE builds ADD COLUMN build_seconds INTEGER;
ALTER TABLE builds ADD COLUMN deploy_seconds INTEGER;

-- +goose Down
ALTER TABLE builds DROP COLUMN discover_seconds;
ALTER TABLE builds DROP COLUMN update_seconds;
ALTER TABLE builds DROP COLUMN build_seconds;
ALTER TABLE builds DROP COLUMN deploy_seconds;
