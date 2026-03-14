-- +goose Up
CREATE TABLE builds (
    id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    duration_seconds INTEGER,
    packages_total INTEGER NOT NULL DEFAULT 0,
    packages_changed INTEGER NOT NULL DEFAULT 0,
    packages_skipped INTEGER NOT NULL DEFAULT 0,
    provider_groups INTEGER NOT NULL DEFAULT 0,
    artifact_count INTEGER NOT NULL DEFAULT 0,
    root_hash TEXT,
    sync_run_id INTEGER,
    status TEXT NOT NULL,
    manifest_json TEXT NOT NULL DEFAULT '{}'
);

-- +goose Down
DROP TABLE builds;
