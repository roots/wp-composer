-- +goose Up
ALTER TABLE packages ADD COLUMN og_image_generated_at TEXT;
ALTER TABLE packages ADD COLUMN og_image_installs INTEGER NOT NULL DEFAULT 0;
ALTER TABLE packages ADD COLUMN og_image_wp_installs INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE packages DROP COLUMN og_image_generated_at;
ALTER TABLE packages DROP COLUMN og_image_installs;
ALTER TABLE packages DROP COLUMN og_image_wp_installs;
