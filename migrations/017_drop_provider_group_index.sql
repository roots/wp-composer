-- +goose Up
DROP INDEX IF EXISTS idx_packages_provider_group;

-- +goose Down
CREATE INDEX idx_packages_provider_group ON packages(provider_group);
