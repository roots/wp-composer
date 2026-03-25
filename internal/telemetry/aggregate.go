package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/roots/wp-packages/internal/packages"
)

const (
	// metaKeyWatermark is the site_meta key for the last processed event ID.
	metaKeyWatermark = "aggregate_watermark"

	// retentionDays controls how long raw events are kept after aggregation.
	// Must be > 30 so the 30d window query has full data. Set conservatively
	// high since incremental aggregation decouples retention from performance.
	retentionDays = 365
)

// AggregateInstalls incrementally aggregates install events into monthly_installs
// and recomputes per-package counters. Only events newer than the stored watermark
// are processed, keeping runtime proportional to new events rather than total history.
func AggregateInstalls(ctx context.Context, db *sql.DB) (AggregateResult, error) {
	now := time.Now().UTC()
	thirtyDaysAgo := now.AddDate(0, 0, -30).Format(time.RFC3339)

	// 1. Read watermark (last processed event ID)
	watermark, err := readWatermark(ctx, db)
	if err != nil {
		return AggregateResult{}, err
	}

	// 2. Find the new high-water mark
	var newWatermark int64
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(id), 0) FROM install_events`,
	).Scan(&newWatermark); err != nil {
		return AggregateResult{}, fmt.Errorf("reading max event id: %w", err)
	}

	// 3. Incrementally upsert monthly counts and update last_installed_at
	//    from new events only. Wrap with watermark save in a transaction so
	//    we never double-count if the process crashes mid-run.
	if newWatermark > watermark {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("beginning incremental tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		_, err = tx.ExecContext(ctx, `
			INSERT INTO monthly_installs (package_id, month, installs)
			SELECT package_id,
				strftime('%Y-%m', created_at, 'utc') AS month,
				COUNT(*) AS installs
			FROM install_events
			WHERE id > ?
			GROUP BY package_id, strftime('%Y-%m', created_at, 'utc')
			ON CONFLICT(package_id, month) DO UPDATE SET installs = installs + excluded.installs`,
			watermark)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("upserting monthly installs: %w", err)
		}

		// Update last_installed_at only if we have a newer timestamp
		_, err = tx.ExecContext(ctx, `
			UPDATE packages SET last_installed_at = sub.last_at
			FROM (
				SELECT package_id, MAX(created_at) AS last_at
				FROM install_events
				WHERE id > ?
				GROUP BY package_id
			) sub
			WHERE packages.id = sub.package_id
			AND (packages.last_installed_at IS NULL OR sub.last_at > packages.last_installed_at)`,
			watermark)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("updating last_installed_at: %w", err)
		}

		// Save watermark inside the same transaction
		_, err = tx.ExecContext(ctx, `
			INSERT INTO site_meta (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			metaKeyWatermark, strconv.FormatInt(newWatermark, 10))
		if err != nil {
			return AggregateResult{}, fmt.Errorf("saving watermark: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return AggregateResult{}, fmt.Errorf("committing incremental aggregation: %w", err)
		}
	}

	// 4. Recompute totals from monthly_installs (always fast — bounded by
	//    ~65k packages × months of history, not raw event count)
	totalResult, err := db.ExecContext(ctx, `
		UPDATE packages SET wp_packages_installs_total = sub.total
		FROM (
			SELECT package_id, SUM(installs) AS total
			FROM monthly_installs
			GROUP BY package_id
		) sub
		WHERE packages.id = sub.package_id`)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("updating total installs: %w", err)
	}
	totalUpdated, _ := totalResult.RowsAffected()

	// Reset totals for packages with no monthly data
	_, err = db.ExecContext(ctx, `
		UPDATE packages SET
			wp_packages_installs_total = 0,
			last_installed_at = NULL
		WHERE (wp_packages_installs_total > 0 OR last_installed_at IS NOT NULL)
		AND id NOT IN (SELECT DISTINCT package_id FROM monthly_installs)`)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("resetting stale total counts: %w", err)
	}

	// 5. Recompute 30d counts from install_events (bounded by retention window)
	_, err = db.ExecContext(ctx, `
		UPDATE packages SET wp_packages_installs_30d = sub.recent
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

	// Reset 30d for packages with no recent events
	resetResult, err := db.ExecContext(ctx, `
		UPDATE packages SET wp_packages_installs_30d = 0
		WHERE wp_packages_installs_30d > 0
		AND id NOT IN (
			SELECT DISTINCT package_id FROM install_events WHERE created_at >= ?
		)`, thirtyDaysAgo)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("resetting stale 30d counts: %w", err)
	}
	resetCount, _ := resetResult.RowsAffected()

	// 6. Prune old events beyond the retention window
	retentionCutoff := now.AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	pruneResult, err := db.ExecContext(ctx, `
		DELETE FROM install_events WHERE created_at < ?`, retentionCutoff)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("pruning old events: %w", err)
	}
	eventsPruned, _ := pruneResult.RowsAffected()

	if err := packages.RefreshSiteStats(ctx, db); err != nil {
		return AggregateResult{}, err
	}

	return AggregateResult{
		PackagesUpdated: totalUpdated,
		PackagesReset:   resetCount,
		EventsPruned:    eventsPruned,
	}, nil
}

// AggregateResult holds the outcome of an aggregation run.
type AggregateResult struct {
	PackagesUpdated int64
	PackagesReset   int64
	EventsPruned    int64
}

func readWatermark(ctx context.Context, db *sql.DB) (int64, error) {
	val, err := packages.GetMeta(ctx, db, metaKeyWatermark)
	if err != nil {
		return 0, fmt.Errorf("reading watermark: %w", err)
	}
	if val == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing watermark %q: %w", val, err)
	}
	return n, nil
}
