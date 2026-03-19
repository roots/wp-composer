package packages

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/roots/wp-packages/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}

	// Create packages table inline (avoid embed dependency in tests)
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
			wp_packages_installs_total INTEGER NOT NULL DEFAULT 0,
			wp_packages_installs_30d INTEGER NOT NULL DEFAULT 0,
			last_installed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(type, name)
		);
			CREATE TABLE sync_runs (
				id INTEGER PRIMARY KEY,
				started_at TEXT NOT NULL,
				finished_at TEXT,
				status TEXT NOT NULL,
				meta_json TEXT NOT NULL DEFAULT '{}'
			);
			CREATE TABLE package_stats (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				active_plugins INTEGER NOT NULL DEFAULT 0,
				active_themes INTEGER NOT NULL DEFAULT 0,
				plugin_installs INTEGER NOT NULL DEFAULT 0,
				theme_installs INTEGER NOT NULL DEFAULT 0,
				installs_30d INTEGER NOT NULL DEFAULT 0,
				updated_at TEXT NOT NULL DEFAULT ''
			);
		`)
	if err != nil {
		t.Fatalf("creating tables: %v", err)
	}

	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestUpsertShellPackage(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	lc := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	err := UpsertShellPackage(ctx, database, "plugin", "akismet", &lc)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Verify row exists
	var name string
	var isActive int
	err = database.QueryRow("SELECT name, is_active FROM packages WHERE type='plugin' AND name='akismet'").Scan(&name, &isActive)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if name != "akismet" || isActive != 1 {
		t.Errorf("got name=%s active=%d", name, isActive)
	}

	// Upsert with older date should not update last_committed
	olderLC := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	err = UpsertShellPackage(ctx, database, "plugin", "akismet", &olderLC)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var lastCommitted string
	_ = database.QueryRow("SELECT last_committed FROM packages WHERE name='akismet'").Scan(&lastCommitted)
	if lastCommitted != "2026-01-15T00:00:00Z" {
		t.Errorf("last_committed should not have been overwritten, got %s", lastCommitted)
	}

	// Upsert with newer date should update
	newerLC := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	err = UpsertShellPackage(ctx, database, "plugin", "akismet", &newerLC)
	if err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	_ = database.QueryRow("SELECT last_committed FROM packages WHERE name='akismet'").Scan(&lastCommitted)
	if lastCommitted != "2026-03-01T00:00:00Z" {
		t.Errorf("last_committed should have been updated, got %s", lastCommitted)
	}
}

func TestUpsertPackage(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	ver := "5.0"
	pkg := &Package{
		Type:           "plugin",
		Name:           "akismet",
		VersionsJSON:   `{"5.0":"https://example.com/5.0.zip"}`,
		CurrentVersion: &ver,
		IsActive:       true,
		Downloads:      1000,
	}

	err := UpsertPackage(ctx, database, pkg)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var downloads int64
	_ = database.QueryRow("SELECT downloads FROM packages WHERE name='akismet'").Scan(&downloads)
	if downloads != 1000 {
		t.Errorf("downloads = %d, want 1000", downloads)
	}

	// Update same package
	pkg.Downloads = 2000
	err = UpsertPackage(ctx, database, pkg)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	_ = database.QueryRow("SELECT downloads FROM packages WHERE name='akismet'").Scan(&downloads)
	if downloads != 2000 {
		t.Errorf("downloads = %d, want 2000", downloads)
	}
}

func TestDeactivatePackage(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	pkg := &Package{
		Type:         "plugin",
		Name:         "dead-plugin",
		VersionsJSON: "{}",
		IsActive:     true,
	}
	_ = UpsertPackage(ctx, database, pkg)

	var id int64
	_ = database.QueryRow("SELECT id FROM packages WHERE name='dead-plugin'").Scan(&id)

	err := DeactivatePackage(ctx, database, id)
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	var isActive int
	_ = database.QueryRow("SELECT is_active FROM packages WHERE id=?", id).Scan(&isActive)
	if isActive != 0 {
		t.Error("package should be inactive")
	}
}

func TestRefreshSiteStats(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	cur := "1.0.0"
	p1 := &Package{
		Type:                    "plugin",
		Name:                    "plugin-one",
		VersionsJSON:            "{}",
		CurrentVersion:          &cur,
		IsActive:                true,
		WpPackagesInstallsTotal: 100,
		WpPackagesInstalls30d:   25,
	}
	p2 := &Package{
		Type:                    "plugin",
		Name:                    "plugin-two",
		VersionsJSON:            "{}",
		CurrentVersion:          &cur,
		IsActive:                false,
		WpPackagesInstallsTotal: 999,
		WpPackagesInstalls30d:   999,
	}
	t1 := &Package{
		Type:                    "theme",
		Name:                    "theme-one",
		VersionsJSON:            "{}",
		CurrentVersion:          &cur,
		IsActive:                true,
		WpPackagesInstallsTotal: 50,
		WpPackagesInstalls30d:   5,
	}

	if err := UpsertPackage(ctx, database, p1); err != nil {
		t.Fatalf("upserting plugin-one: %v", err)
	}
	if err := UpsertPackage(ctx, database, p2); err != nil {
		t.Fatalf("upserting plugin-two: %v", err)
	}
	if err := UpsertPackage(ctx, database, t1); err != nil {
		t.Fatalf("upserting theme-one: %v", err)
	}

	// Install counters are maintained by telemetry aggregation, not package upserts.
	if _, err := database.Exec(`UPDATE packages SET wp_packages_installs_total = 100, wp_packages_installs_30d = 25 WHERE name = 'plugin-one'`); err != nil {
		t.Fatalf("updating plugin-one counters: %v", err)
	}
	if _, err := database.Exec(`UPDATE packages SET wp_packages_installs_total = 999, wp_packages_installs_30d = 999 WHERE name = 'plugin-two'`); err != nil {
		t.Fatalf("updating plugin-two counters: %v", err)
	}
	if _, err := database.Exec(`UPDATE packages SET wp_packages_installs_total = 50, wp_packages_installs_30d = 5 WHERE name = 'theme-one'`); err != nil {
		t.Fatalf("updating theme-one counters: %v", err)
	}

	if err := RefreshSiteStats(ctx, database); err != nil {
		t.Fatalf("refreshing site stats: %v", err)
	}

	var activePlugins, activeThemes, pluginInstalls, themeInstalls, installs30d int
	err := database.QueryRow(`SELECT active_plugins, active_themes, plugin_installs, theme_installs, installs_30d FROM package_stats WHERE id = 1`).Scan(
		&activePlugins, &activeThemes, &pluginInstalls, &themeInstalls, &installs30d,
	)
	if err != nil {
		t.Fatalf("querying package_stats: %v", err)
	}

	if activePlugins != 1 {
		t.Errorf("active_plugins = %d, want 1", activePlugins)
	}
	if activeThemes != 1 {
		t.Errorf("active_themes = %d, want 1", activeThemes)
	}
	if pluginInstalls != 100 {
		t.Errorf("plugin_installs = %d, want 100", pluginInstalls)
	}
	if themeInstalls != 50 {
		t.Errorf("theme_installs = %d, want 50", themeInstalls)
	}
	if installs30d != 30 {
		t.Errorf("installs_30d = %d, want 30", installs30d)
	}
}

func TestGetPackagesNeedingUpdate(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	// Package with no last_synced_at — needs update
	lc := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = UpsertShellPackage(ctx, database, "plugin", "needs-update", &lc)

	// Package already synced with matching date — does not need update
	synced := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	pkg := &Package{
		Type:          "plugin",
		Name:          "up-to-date",
		VersionsJSON:  "{}",
		IsActive:      true,
		LastCommitted: &lc,
		LastSyncedAt:  &synced,
	}
	_ = UpsertPackage(ctx, database, pkg)

	pkgs, err := GetPackagesNeedingUpdate(ctx, database, UpdateQueryOpts{Type: "plugin"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package needing update, got %d", len(pkgs))
	}
	if pkgs[0].Name != "needs-update" {
		t.Errorf("got name=%s, want needs-update", pkgs[0].Name)
	}
}

func TestBatchUpsertShellPackages(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	lc1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	lc2 := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	entries := []ShellEntry{
		{Type: "plugin", Name: "akismet", LastCommitted: &lc1},
		{Type: "theme", Name: "astra", LastCommitted: &lc2},
	}

	err := BatchUpsertShellPackages(ctx, database, entries)
	if err != nil {
		t.Fatalf("batch upsert: %v", err)
	}

	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 packages, got %d", count)
	}

	// Verify individual values
	var name string
	var isActive int
	_ = database.QueryRow("SELECT name, is_active FROM packages WHERE type='plugin' AND name='akismet'").Scan(&name, &isActive)
	if name != "akismet" || isActive != 1 {
		t.Errorf("got name=%s active=%d", name, isActive)
	}

	// Batch upsert with older dates should not overwrite
	olderLC := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	entries2 := []ShellEntry{
		{Type: "plugin", Name: "akismet", LastCommitted: &olderLC},
	}
	err = BatchUpsertShellPackages(ctx, database, entries2)
	if err != nil {
		t.Fatalf("second batch upsert: %v", err)
	}

	var lastCommitted string
	_ = database.QueryRow("SELECT last_committed FROM packages WHERE name='akismet'").Scan(&lastCommitted)
	if lastCommitted != "2026-01-15T00:00:00Z" {
		t.Errorf("last_committed should not have been overwritten, got %s", lastCommitted)
	}
}

func TestBatchUpsertShellPackagesEmpty(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	err := BatchUpsertShellPackages(ctx, database, nil)
	if err != nil {
		t.Fatalf("empty batch upsert should not fail: %v", err)
	}

	err = BatchUpsertShellPackages(ctx, database, []ShellEntry{})
	if err != nil {
		t.Fatalf("empty slice batch upsert should not fail: %v", err)
	}

	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 packages, got %d", count)
	}
}

func TestBatchUpsertPackages(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	ver1 := "5.0"
	ver2 := "4.0"
	pkgs := []*Package{
		{
			Type:           "plugin",
			Name:           "akismet",
			VersionsJSON:   `{"5.0":"https://example.com/5.0.zip"}`,
			CurrentVersion: &ver1,
			IsActive:       true,
			Downloads:      1000,
		},
		{
			Type:           "theme",
			Name:           "astra",
			VersionsJSON:   `{"4.0":"https://example.com/4.0.zip"}`,
			CurrentVersion: &ver2,
			IsActive:       true,
			Downloads:      500,
		},
	}

	err := BatchUpsertPackages(ctx, database, pkgs)
	if err != nil {
		t.Fatalf("batch upsert: %v", err)
	}

	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 packages, got %d", count)
	}

	// Update and re-upsert
	pkgs[0].Downloads = 2000
	err = BatchUpsertPackages(ctx, database, pkgs[:1])
	if err != nil {
		t.Fatalf("second batch upsert: %v", err)
	}

	var downloads int64
	_ = database.QueryRow("SELECT downloads FROM packages WHERE name='akismet'").Scan(&downloads)
	if downloads != 2000 {
		t.Errorf("downloads = %d, want 2000", downloads)
	}

	// Empty batch should be no-op
	err = BatchUpsertPackages(ctx, database, nil)
	if err != nil {
		t.Fatalf("empty batch should not fail: %v", err)
	}
}

func TestAllocateSyncRunID(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	run1, err := AllocateSyncRunID(ctx, database)
	if err != nil {
		t.Fatalf("first alloc: %v", err)
	}
	if run1.RunID != 1 {
		t.Errorf("first run ID = %d, want 1", run1.RunID)
	}

	// Simulate a package with sync_run_id=1
	_, _ = database.Exec("INSERT INTO packages (type, name, versions_json, last_sync_run_id, created_at, updated_at) VALUES ('plugin', 'test', '{}', 1, datetime('now'), datetime('now'))")

	run2, err := AllocateSyncRunID(ctx, database)
	if err != nil {
		t.Fatalf("second alloc: %v", err)
	}
	if run2.RunID != 2 {
		t.Errorf("second run ID = %d, want 2", run2.RunID)
	}

	// Verify sync_runs rows
	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM sync_runs").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 sync_runs rows, got %d", count)
	}

	// FinishSyncRun should work
	err = FinishSyncRun(ctx, database, run2.RowID, "completed", map[string]any{"updated": 5})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	var status string
	_ = database.QueryRow("SELECT status FROM sync_runs WHERE id=?", run2.RowID).Scan(&status)
	if status != "completed" {
		t.Errorf("status = %q, want completed", status)
	}
}
