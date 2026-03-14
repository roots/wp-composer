-- +goose Up
CREATE TABLE install_events (
    id INTEGER PRIMARY KEY,
    package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    ip_hash TEXT NOT NULL,
    user_agent_hash TEXT NOT NULL,
    dedupe_bucket INTEGER NOT NULL,
    dedupe_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(dedupe_hash, dedupe_bucket)
);

CREATE INDEX idx_install_events_package_created ON install_events(package_id, created_at DESC);
CREATE INDEX idx_install_events_created ON install_events(created_at DESC);

-- +goose Down
DROP INDEX idx_install_events_created;
DROP INDEX idx_install_events_package_created;
DROP TABLE install_events;
