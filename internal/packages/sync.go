package packages

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SyncRun holds both the logical sync run ID (for package tracking) and the
// database row ID (for updating sync_runs status).
type SyncRun struct {
	RowID int64 // sync_runs.id (auto-increment)
	RunID int64 // logical ID assigned to packages.last_sync_run_id
}

// AllocateSyncRunID atomically allocates a new sync run ID and creates a sync_runs record.
func AllocateSyncRunID(ctx context.Context, db *sql.DB) (*SyncRun, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var runID int64
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(last_sync_run_id), 0) + 1 FROM packages`,
	).Scan(&runID)
	if err != nil {
		return nil, fmt.Errorf("allocating sync run id: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx,
		`INSERT INTO sync_runs (started_at, status) VALUES (?, ?)`,
		now, "running",
	)
	if err != nil {
		return nil, fmt.Errorf("inserting sync run: %w", err)
	}

	rowID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting sync run row id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing sync run: %w", err)
	}

	return &SyncRun{RowID: rowID, RunID: runID}, nil
}

// FinishSyncRun marks a sync run as completed with stats.
func FinishSyncRun(ctx context.Context, db *sql.DB, rowID int64, status string, stats map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339)
	metaJSON, _ := json.Marshal(stats)

	_, err := db.ExecContext(ctx,
		`UPDATE sync_runs SET finished_at = ?, status = ?, meta_json = ? WHERE id = ?`,
		now, status, string(metaJSON), rowID,
	)
	if err != nil {
		return fmt.Errorf("finishing sync run %d: %w", rowID, err)
	}
	return nil
}
