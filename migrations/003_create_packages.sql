-- +goose Up
CREATE TABLE packages (
    id INTEGER PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('plugin','theme')),
    name TEXT NOT NULL,
    display_name TEXT,
    description TEXT,
    author TEXT,
    homepage TEXT,
    slug_url TEXT,
    provider_group TEXT,
    versions_json TEXT NOT NULL DEFAULT '{}',
    downloads INTEGER NOT NULL DEFAULT 0,
    active_installs INTEGER NOT NULL DEFAULT 0,
    current_version TEXT,
    rating REAL,
    num_ratings INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1,
    last_committed TEXT,
    last_synced_at TEXT,
    last_sync_run_id INTEGER,
    wp_composer_installs_total INTEGER NOT NULL DEFAULT 0,
    wp_composer_installs_30d INTEGER NOT NULL DEFAULT 0,
    last_installed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(type, name)
);

CREATE INDEX idx_packages_active_type_downloads ON packages(is_active, type, downloads DESC);
CREATE INDEX idx_packages_search_name ON packages(name);
CREATE INDEX idx_packages_provider_group ON packages(provider_group);
CREATE INDEX idx_packages_sync_run ON packages(last_sync_run_id);

-- +goose Down
DROP INDEX idx_packages_sync_run;
DROP INDEX idx_packages_provider_group;
DROP INDEX idx_packages_search_name;
DROP INDEX idx_packages_active_type_downloads;
DROP TABLE packages;
