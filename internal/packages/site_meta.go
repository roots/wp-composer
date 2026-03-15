package packages

import (
	"context"
	"database/sql"
	"fmt"
)

// GetMeta retrieves a value from site_meta by key. Returns "" if not found.
func GetMeta(ctx context.Context, db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM site_meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting site_meta %q: %w", key, err)
	}
	return value, nil
}

// SetMeta inserts or updates a value in site_meta.
func SetMeta(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO site_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	if err != nil {
		return fmt.Errorf("setting site_meta %q: %w", key, err)
	}
	return nil
}
