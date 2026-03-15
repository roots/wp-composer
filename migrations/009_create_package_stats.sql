-- +goose Up
CREATE TABLE package_stats (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    active_plugins INTEGER NOT NULL DEFAULT 0,
    active_themes INTEGER NOT NULL DEFAULT 0,
    plugin_installs INTEGER NOT NULL DEFAULT 0,
    theme_installs INTEGER NOT NULL DEFAULT 0,
    installs_30d INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

INSERT INTO package_stats (id, active_plugins, active_themes, plugin_installs, theme_installs, installs_30d, updated_at)
    SELECT 1,
        SUM(CASE WHEN type = 'plugin' THEN 1 ELSE 0 END),
        SUM(CASE WHEN type = 'theme' THEN 1 ELSE 0 END),
        COALESCE(SUM(CASE WHEN type = 'plugin' THEN wp_composer_installs_total ELSE 0 END), 0),
        COALESCE(SUM(CASE WHEN type = 'theme' THEN wp_composer_installs_total ELSE 0 END), 0),
        COALESCE(SUM(wp_composer_installs_30d), 0),
        datetime('now')
    FROM packages
    WHERE is_active = 1;

-- +goose Down
DROP TABLE package_stats;
