package telemetry

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/roots/wp-packages/internal/composer"
)

// RecordInstall inserts an install event with bucket-based deduplication.
// Uses INSERT OR IGNORE with the UNIQUE(dedupe_hash, dedupe_bucket) constraint.
// Returns true if a new row was inserted, false if deduplicated.
func RecordInstall(ctx context.Context, db *sql.DB, params InstallParams, dedupeWindowSeconds int) (bool, error) {
	if dedupeWindowSeconds <= 0 {
		dedupeWindowSeconds = 3600
	}
	now := time.Now().UTC()
	bucket := now.Unix() / int64(dedupeWindowSeconds)
	dedupeHash := computeDedupeHash(params.IPHash, params.PackageID, params.Version, params.UserAgentHash)

	result, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO install_events
			(package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		params.PackageID, params.Version, params.IPHash, params.UserAgentHash,
		bucket, dedupeHash, now.Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("inserting install event: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking rows affected: %w", err)
	}
	return rows > 0, nil
}

// InstallParams holds the parameters for recording an install event.
type InstallParams struct {
	PackageID     int64
	Version       string
	IPHash        string
	UserAgentHash string
}

// HashIP returns the SHA-256 hex digest of an IP address string.
func HashIP(ip string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
}

// HashUserAgent returns the SHA-256 hex digest of a User-Agent string.
func HashUserAgent(ua string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ua)))
}

func computeDedupeHash(ipHash string, packageID int64, version, userAgentHash string) string {
	input := fmt.Sprintf("%s%d%s%s", ipHash, packageID, version, userAgentHash)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// LookupPackageID finds the package ID for a Composer package name (e.g. "wp-plugin/akismet").
// Returns 0 if not found or inactive.
func LookupPackageID(ctx context.Context, db *sql.DB, composerName string) (int64, error) {
	parts := strings.SplitN(composerName, "/", 2)
	if len(parts) != 2 {
		return 0, nil
	}

	vendor := parts[0]
	slug := parts[1]

	pkgType := composer.PackageType(vendor)
	if pkgType == "" {
		return 0, nil
	}

	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM packages WHERE type = ? AND name = ? AND is_active = 1`,
		pkgType, slug,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("looking up package %s: %w", composerName, err)
	}
	return id, nil
}
