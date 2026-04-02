package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsSQLiteBusy(t *testing.T) {
	if IsSQLiteBusy(nil) {
		t.Error("expected nil to return false")
	}
	if IsSQLiteBusy(os.ErrNotExist) {
		t.Error("expected non-sqlite error to return false")
	}
}

func TestIsSQLiteBusy_RealContention(t *testing.T) {
	// Create a file-backed DB so two connections can contend.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db1.Close() }()

	// Use default journal mode (not WAL) and zero busy timeout to force immediate SQLITE_BUSY.
	_, _ = db1.Exec("PRAGMA busy_timeout = 0")
	_, _ = db1.Exec("PRAGMA journal_mode = DELETE")
	_, _ = db1.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, val TEXT)")
	_, _ = db1.Exec("INSERT INTO test VALUES (1, 'a')")

	db2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	_, _ = db2.Exec("PRAGMA busy_timeout = 0")
	_, _ = db2.Exec("PRAGMA journal_mode = DELETE")

	// db1 holds a write transaction.
	tx, err := db1.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, _ = tx.Exec("UPDATE test SET val = 'b' WHERE id = 1")

	// db2 tries to write — should get SQLITE_BUSY.
	_, err = db2.Exec("UPDATE test SET val = 'c' WHERE id = 1")
	if err == nil {
		t.Fatal("expected SQLITE_BUSY error, got nil")
	}
	if !IsSQLiteBusy(err) {
		t.Errorf("expected IsSQLiteBusy=true, got false for error: %v", err)
	}
}

func TestBeginTxRetry_ContextCancel(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, _ = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY)")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err = BeginTxRetry(ctx, db, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error with cancelled context")
	}
	if elapsed > 1*time.Second {
		t.Errorf("should not have retried with cancelled context, took %v", elapsed)
	}
}

func TestExecRetry_Success(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, _ = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, val TEXT)")

	_, err = ExecRetry(context.Background(), db, "INSERT INTO test VALUES (1, 'hello')")
	if err != nil {
		t.Fatalf("ExecRetry failed: %v", err)
	}

	var val string
	_ = db.QueryRow("SELECT val FROM test WHERE id = 1").Scan(&val)
	if val != "hello" {
		t.Errorf("val = %q, want 'hello'", val)
	}
}
