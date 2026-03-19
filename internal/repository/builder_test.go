package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/roots/wp-packages/internal/db"
)

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
			author TEXT,
			homepage TEXT,
			slug_url TEXT,
			provider_group TEXT,
			versions_json TEXT NOT NULL DEFAULT '{}',
			downloads INTEGER NOT NULL DEFAULT 0,
			active_installs INTEGER NOT NULL DEFAULT 0,
			current_version TEXT,
			rating REAL,
			num_ratings INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			last_committed TEXT,
			last_synced_at TEXT,
			last_sync_run_id INTEGER,
			wp_composer_installs_total INTEGER NOT NULL DEFAULT 0,
			wp_composer_installs_30d INTEGER NOT NULL DEFAULT 0,
			last_installed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(type, name)
		)`)
	if err != nil {
		t.Fatalf("creating table: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestBuild(t *testing.T) {
	database := setupTestDB(t)

	// Insert test packages
	_, _ = database.Exec(`INSERT INTO packages (type, name, display_name, versions_json, is_active, last_sync_run_id, created_at, updated_at)
		VALUES ('plugin', 'akismet', 'Akismet',
			'{"5.0":"https://downloads.wordpress.org/plugin/akismet.5.0.zip","4.0":"https://downloads.wordpress.org/plugin/akismet.4.0.zip"}',
			1, 1, datetime('now'), datetime('now'))`)
	_, _ = database.Exec(`INSERT INTO packages (type, name, display_name, versions_json, is_active, last_sync_run_id, created_at, updated_at)
		VALUES ('theme', 'astra', 'Astra',
			'{"4.0":"https://downloads.wordpress.org/theme/astra.4.0.zip"}',
			1, 1, datetime('now'), datetime('now'))`)

	tmpDir := t.TempDir()

	result, err := Build(context.Background(), database, BuildOpts{
		OutputDir: tmpDir,
		AppURL:    "https://app.example.com",
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if result.PackagesTotal != 2 {
		t.Errorf("packages_total = %d, want 2", result.PackagesTotal)
	}

	// Verify packages.json exists and is valid
	packagesPath := filepath.Join(result.BuildDir, "packages.json")
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		t.Fatalf("packages.json missing: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("packages.json invalid: %v", err)
	}

	// Check notify-batch is absolute
	notifyBatch, ok := root["notify-batch"].(string)
	if !ok || notifyBatch != "https://app.example.com/downloads" {
		t.Errorf("notify-batch = %q, want https://app.example.com/downloads", notifyBatch)
	}

	// packages.json should NOT contain provider-includes or providers-url
	if _, ok := root["provider-includes"]; ok {
		t.Error("packages.json should not contain provider-includes")
	}
	if _, ok := root["providers-url"]; ok {
		t.Error("packages.json should not contain providers-url")
	}

	// Check metadata-url
	if root["metadata-url"] != "/p2/%package%.json" {
		t.Errorf("metadata-url = %q, want /p2/%%package%%.json", root["metadata-url"])
	}

	// Check p2 files exist
	for _, path := range []string{"p2/wp-plugin/akismet.json", "p2/wp-theme/astra.json"} {
		if _, err := os.Stat(filepath.Join(result.BuildDir, path)); err != nil {
			t.Errorf("p2 file missing: %s", path)
		}
	}

	// p/ directory should NOT exist
	if _, err := os.Stat(filepath.Join(result.BuildDir, "p")); !os.IsNotExist(err) {
		t.Error("p/ directory should not exist")
	}

	// Check manifest.json
	manifestData, err := os.ReadFile(filepath.Join(result.BuildDir, "manifest.json"))
	if err != nil {
		t.Fatal("manifest.json missing")
	}
	var manifest map[string]any
	_ = json.Unmarshal(manifestData, &manifest)
	if manifest["packages_total"].(float64) != 2 {
		t.Errorf("manifest packages_total = %v", manifest["packages_total"])
	}

	// Integrity validation should pass
	errors := ValidateIntegrity(result.BuildDir)
	if len(errors) > 0 {
		t.Errorf("integrity validation failed: %v", errors)
	}
}

func TestBuildParallelWrites(t *testing.T) {
	database := setupTestDB(t)

	// Insert many packages to exercise parallel writes
	for i := 0; i < 20; i++ {
		slug := fmt.Sprintf("plugin-%d", i)
		_, _ = database.Exec(`INSERT INTO packages (type, name, display_name, versions_json, is_active, last_sync_run_id, created_at, updated_at)
			VALUES ('plugin', ?, ?,
				'{"1.0":"https://downloads.wordpress.org/plugin/test.1.0.zip"}',
				1, 1, datetime('now'), datetime('now'))`, slug, slug)
	}

	tmpDir := t.TempDir()
	result, err := Build(context.Background(), database, BuildOpts{
		OutputDir: tmpDir,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if result.PackagesTotal != 20 {
		t.Errorf("packages_total = %d, want 20", result.PackagesTotal)
	}

	// Verify p2 files exist on disk
	for i := 0; i < 20; i++ {
		slug := fmt.Sprintf("plugin-%d", i)
		p2Path := filepath.Join(result.BuildDir, "p2", "wp-plugin", slug+".json")
		if _, err := os.Stat(p2Path); err != nil {
			t.Errorf("p2 file missing: %s", p2Path)
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	database := setupTestDB(t)
	tmpDir := t.TempDir()

	result, err := Build(context.Background(), database, BuildOpts{
		OutputDir: tmpDir,
		Logger:    slog.Default(),
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if result.PackagesTotal != 0 {
		t.Errorf("expected 0 packages, got %d", result.PackagesTotal)
	}
}
