-- +goose Up
ALTER TABLE packages RENAME COLUMN wp_composer_installs_total TO wp_packages_installs_total;
ALTER TABLE packages RENAME COLUMN wp_composer_installs_30d TO wp_packages_installs_30d;

-- +goose Down
ALTER TABLE packages RENAME COLUMN wp_packages_installs_total TO wp_composer_installs_total;
ALTER TABLE packages RENAME COLUMN wp_packages_installs_30d TO wp_composer_installs_30d;
