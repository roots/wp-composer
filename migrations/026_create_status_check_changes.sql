-- +goose Up
CREATE TABLE status_check_changes (
    id INTEGER PRIMARY KEY,
    status_check_id INTEGER NOT NULL REFERENCES status_checks(id),
    package_type TEXT NOT NULL,
    package_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('deactivated', 'reactivated')),
    created_at TEXT NOT NULL
);
CREATE INDEX idx_status_check_changes_check_id ON status_check_changes(status_check_id);
CREATE INDEX idx_status_check_changes_created_at ON status_check_changes(created_at);

-- +goose Down
DROP TABLE status_check_changes;
