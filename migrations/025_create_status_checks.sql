-- +goose Up
CREATE TABLE IF NOT EXISTS status_checks (
    id INTEGER PRIMARY KEY,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    status TEXT NOT NULL DEFAULT 'running',
    checked INTEGER NOT NULL DEFAULT 0,
    deactivated INTEGER NOT NULL DEFAULT 0,
    reactivated INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    duration_seconds INTEGER,
    error_message TEXT
);

-- +goose Down
DROP TABLE IF EXISTS status_checks;
