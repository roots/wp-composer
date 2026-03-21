-- +goose Up
CREATE TABLE metadata_changes (
    id INTEGER PRIMARY KEY,
    package_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('update', 'delete')),
    timestamp INTEGER NOT NULL,
    build_id TEXT NOT NULL
);
CREATE INDEX idx_metadata_changes_timestamp ON metadata_changes(timestamp);

-- +goose Down
DROP TABLE metadata_changes;
