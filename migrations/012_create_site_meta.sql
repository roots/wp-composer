-- +goose Up
CREATE TABLE IF NOT EXISTS site_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Backfill: any package that was synced but has no last_committed will never be
-- re-synced. Set last_committed = last_synced_at so the next SVN changelog run
-- can move it forward when a real change is detected.
UPDATE packages
SET last_committed = last_synced_at,
    updated_at = datetime('now')
WHERE last_committed IS NULL
  AND last_synced_at IS NOT NULL;

-- +goose Down
DROP TABLE site_meta;
