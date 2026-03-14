-- +goose Up
CREATE TABLE sync_runs (
    id INTEGER PRIMARY KEY,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    status TEXT NOT NULL,
    meta_json TEXT NOT NULL DEFAULT '{}'
);

-- +goose Down
DROP TABLE sync_runs;
