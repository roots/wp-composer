-- +goose Up
CREATE TABLE monthly_installs (
    package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    month TEXT NOT NULL,
    installs INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (package_id, month)
);

-- +goose Down
DROP TABLE monthly_installs;
