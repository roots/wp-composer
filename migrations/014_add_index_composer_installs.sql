-- +goose Up
CREATE INDEX idx_packages_active_composer_installs ON packages(is_active, wp_composer_installs_total DESC);
CREATE INDEX idx_packages_active_last_committed ON packages(is_active, last_committed DESC);

-- +goose Down
DROP INDEX idx_packages_active_last_committed;
DROP INDEX idx_packages_active_composer_installs;
