package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AggregateInstalls recomputes wp_composer_installs_total, wp_composer_installs_30d,
// and last_installed_at on all packages.
func AggregateInstalls(ctx context.Context, db *sql.DB) (AggregateResult, error) {
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)

	// Update total counts and last_installed_at
	totalResult, err := db.ExecContext(ctx, `
		UPDATE packages SET
			wp_composer_installs_total = sub.total,
			last_installed_at = sub.last_at
		FROM (
			SELECT package_id, COUNT(*) AS total, MAX(created_at) AS last_at
			FROM install_events
			GROUP BY package_id
		) sub
		WHERE packages.id = sub.package_id`)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("updating total installs: %w", err)
	}
	totalUpdated, _ := totalResult.RowsAffected()

	// Update 30-day counts
	_, err = db.ExecContext(ctx, `
		UPDATE packages SET
			wp_composer_installs_30d = sub.recent
		FROM (
			SELECT package_id, COUNT(*) AS recent
			FROM install_events
			WHERE created_at >= ?
			GROUP BY package_id
		) sub
		WHERE packages.id = sub.package_id`, thirtyDaysAgo)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("updating 30d installs: %w", err)
	}

	// Reset totals for packages with no events at all
	_, err = db.ExecContext(ctx, `
		UPDATE packages SET
			wp_composer_installs_total = 0,
			last_installed_at = NULL
		WHERE (wp_composer_installs_total > 0 OR last_installed_at IS NOT NULL)
		AND id NOT IN (
			SELECT DISTINCT package_id FROM install_events
		)`)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("resetting stale total counts: %w", err)
	}

	// Reset 30d counts for packages with no recent installs
	resetResult, err := db.ExecContext(ctx, `
		UPDATE packages SET wp_composer_installs_30d = 0
		WHERE wp_composer_installs_30d > 0
		AND id NOT IN (
			SELECT DISTINCT package_id FROM install_events WHERE created_at >= ?
		)`, thirtyDaysAgo)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("resetting stale 30d counts: %w", err)
	}
	resetCount, _ := resetResult.RowsAffected()

	return AggregateResult{
		PackagesUpdated: totalUpdated,
		PackagesReset:   resetCount,
	}, nil
}

// AggregateResult holds the outcome of an aggregation run.
type AggregateResult struct {
	PackagesUpdated int64
	PackagesReset   int64
}
