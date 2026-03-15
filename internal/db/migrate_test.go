package db

import (
	"database/sql"
	"testing"

	wpcomposergo "github.com/roots/wp-composer"
)

func TestMigrateCreatesPackageStatsAndFTS(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := Migrate(database, wpcomposergo.Migrations); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	assertObjectExists(t, database, "table", "package_stats")
	assertObjectExists(t, database, "table", "packages_fts")
	assertObjectExists(t, database, "trigger", "packages_fts_insert")
	assertObjectExists(t, database, "trigger", "packages_fts_update")
	assertObjectExists(t, database, "trigger", "packages_fts_delete")
}

func assertObjectExists(t *testing.T, database *sql.DB, objType, name string) {
	t.Helper()
	var count int
	err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, objType, name).Scan(&count)
	if err != nil {
		t.Fatalf("querying sqlite_master for %s %s: %v", objType, name, err)
	}
	if count != 1 {
		t.Fatalf("expected %s %s to exist", objType, name)
	}
}
