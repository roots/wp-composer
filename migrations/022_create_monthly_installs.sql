-- +goose Up
CREATE TABLE monthly_installs (
    package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    month TEXT NOT NULL,
    installs INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (package_id, month)
);

CREATE INDEX idx_monthly_installs_month ON monthly_installs(month);

INSERT INTO monthly_installs (package_id, month, installs)
SELECT package_id, '2026-03', COUNT(*)
FROM install_events
WHERE created_at >= '2026-03-01T00:00:00Z' AND created_at < '2026-04-01T00:00:00Z'
GROUP BY package_id;

-- +goose Down
DROP INDEX idx_monthly_installs_month;
DROP TABLE monthly_installs;
