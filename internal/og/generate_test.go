package og

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	"github.com/roots/wp-packages/internal/db"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func stubRenderer(_ PackageData) ([]byte, error) {
	return []byte("fake-png"), nil
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}

	_, err = database.Exec(`
		CREATE TABLE packages (
			id INTEGER PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('plugin','theme')),
			name TEXT NOT NULL,
			display_name TEXT,
			description TEXT,
			current_version TEXT,
			active_installs INTEGER NOT NULL DEFAULT 0,
			wp_packages_installs_total INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			og_image_generated_at TEXT,
			og_image_installs INTEGER NOT NULL DEFAULT 0,
			og_image_wp_installs INTEGER NOT NULL DEFAULT 0,
			UNIQUE(type, name)
		)`)
	if err != nil {
		t.Fatalf("creating tables: %v", err)
	}

	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertPackage(t *testing.T, database *sql.DB, pkgType, name, displayName, description, version string, installs, wpInstalls int64) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO packages (type, name, display_name, description, current_version, active_installs, wp_packages_installs_total)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, pkgType, name, displayName, description, version, installs, wpInstalls)
	if err != nil {
		t.Fatalf("inserting package: %v", err)
	}
}

func markGenerated(t *testing.T, database *sql.DB, name string, installs, wpInstalls int64) {
	t.Helper()
	_, err := database.Exec(`UPDATE packages SET og_image_generated_at = '2026-01-01T00:00:00Z', og_image_installs = ?, og_image_wp_installs = ? WHERE name = ?`,
		installs, wpInstalls, name)
	if err != nil {
		t.Fatalf("marking generated: %v", err)
	}
}

func TestGenerateNew_GeneratesForNewPackages(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	uploader := &Uploader{localDir: t.TempDir()}

	insertPackage(t, database, "plugin", "akismet", "Akismet", "Anti-spam plugin", "6.0.0", 5000000, 100)
	insertPackage(t, database, "plugin", "woocommerce", "WooCommerce", "eCommerce plugin", "9.6.2", 5000000, 200)

	result, err := GenerateNew(ctx, database, uploader, stubRenderer, testLogger())
	if err != nil {
		t.Fatalf("GenerateNew: %v", err)
	}

	if result.Generated != 2 {
		t.Errorf("expected 2 generated, got %d", result.Generated)
	}

	var genAt *string
	err = database.QueryRow(`SELECT og_image_generated_at FROM packages WHERE name = 'akismet'`).Scan(&genAt)
	if err != nil {
		t.Fatalf("querying og_image_generated_at: %v", err)
	}
	if genAt == nil {
		t.Error("expected og_image_generated_at to be set")
	}
}

func TestGenerateNew_SkipsAlreadyGenerated(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	uploader := &Uploader{localDir: t.TempDir()}

	insertPackage(t, database, "plugin", "akismet", "Akismet", "Anti-spam plugin", "6.0.0", 5000000, 100)
	markGenerated(t, database, "akismet", 5000000, 100)

	insertPackage(t, database, "plugin", "woocommerce", "WooCommerce", "eCommerce plugin", "9.6.2", 5000000, 200)

	result, err := GenerateNew(ctx, database, uploader, stubRenderer, testLogger())
	if err != nil {
		t.Fatalf("GenerateNew: %v", err)
	}

	if result.Generated != 1 {
		t.Errorf("expected 1 generated (woocommerce only), got %d", result.Generated)
	}
}

func TestGenerateNew_NothingToDo(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()
	uploader := &Uploader{localDir: t.TempDir()}

	insertPackage(t, database, "plugin", "akismet", "Akismet", "Anti-spam plugin", "6.0.0", 5000000, 100)
	markGenerated(t, database, "akismet", 5000000, 100)

	result, err := GenerateNew(ctx, database, uploader, stubRenderer, testLogger())
	if err != nil {
		t.Fatalf("GenerateNew: %v", err)
	}

	if result.Generated != 0 {
		t.Errorf("expected 0 generated, got %d", result.Generated)
	}
}
