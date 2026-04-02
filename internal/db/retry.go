package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	busyMaxRetries = 3
	busyRetryDelay = 2 * time.Second
)

// IsSQLiteBusy returns true if the error is a SQLITE_BUSY error.
func IsSQLiteBusy(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == sqlite3.SQLITE_BUSY
	}
	return false
}

// BeginTxRetry wraps db.BeginTx with retry on SQLITE_BUSY.
func BeginTxRetry(ctx context.Context, database *sql.DB, opts *sql.TxOptions) (*sql.Tx, error) {
	var tx *sql.Tx
	var err error
	for attempt := 0; attempt <= busyMaxRetries; attempt++ {
		tx, err = database.BeginTx(ctx, opts)
		if err == nil || !IsSQLiteBusy(err) {
			return tx, err
		}
		if attempt < busyMaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(busyRetryDelay):
			}
		}
	}
	return nil, err
}

// ExecRetry wraps db.ExecContext with retry on SQLITE_BUSY.
func ExecRetry(ctx context.Context, database *sql.DB, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	var err error
	for attempt := 0; attempt <= busyMaxRetries; attempt++ {
		result, err = database.ExecContext(ctx, query, args...)
		if err == nil || !IsSQLiteBusy(err) {
			return result, err
		}
		if attempt < busyMaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(busyRetryDelay):
			}
		}
	}
	return result, err
}
