package telemetry

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

	_, err = database.Exec(`
		CREATE TABLE packages (
			id INTEGER PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('plugin','theme')),
			name TEXT NOT NULL,
			display_name TEXT, description TEXT, author TEXT, homepage TEXT,
			slug_url TEXT,
			versions_json TEXT NOT NULL DEFAULT '{}',
			downloads INTEGER NOT NULL DEFAULT 0,
			active_installs INTEGER NOT NULL DEFAULT 0,
			current_version TEXT, rating REAL,
			num_ratings INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			last_committed TEXT, last_synced_at TEXT, last_sync_run_id INTEGER,
			wp_packages_installs_total INTEGER NOT NULL DEFAULT 0,
			wp_packages_installs_30d INTEGER NOT NULL DEFAULT 0,
			last_installed_at TEXT,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
			UNIQUE(type, name)
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
		INSERT INTO package_stats (id) VALUES (1);
		CREATE TABLE install_events (
			id INTEGER PRIMARY KEY,
			package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
			version TEXT NOT NULL,
			ip_hash TEXT NOT NULL,
			user_agent_hash TEXT NOT NULL,
			dedupe_bucket INTEGER NOT NULL,
			dedupe_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(dedupe_hash, dedupe_bucket)
		);
		CREATE TABLE monthly_installs (
			package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
			month TEXT NOT NULL,
			installs INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (package_id, month)
		);
		CREATE TABLE site_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("creating tables: %v", err)
	}

	// Insert test package
	_, _ = database.Exec(`INSERT INTO packages (type, name, versions_json, is_active, created_at, updated_at)
		VALUES ('plugin', 'akismet', '{}', 1, datetime('now'), datetime('now'))`)
	_, _ = database.Exec(`INSERT INTO packages (type, name, versions_json, is_active, created_at, updated_at)
		VALUES ('theme', 'astra', '{}', 1, datetime('now'), datetime('now'))`)

	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestRecordInstall(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	params := InstallParams{
		PackageID:     1,
		Version:       "5.0",
		IPHash:        HashIP("192.168.1.1"),
		UserAgentHash: HashUserAgent("Composer/2.0"),
	}

	// First insert should succeed
	inserted, err := RecordInstall(ctx, database, params, 3600)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !inserted {
		t.Error("first insert should return true")
	}

	// Duplicate within same bucket should be ignored
	inserted, err = RecordInstall(ctx, database, params, 3600)
	if err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	if inserted {
		t.Error("duplicate should return false")
	}

	// Verify only one row exists
	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM install_events").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

func TestRecordInstall_ZeroDedupeWindowDefaults(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	params := InstallParams{
		PackageID:     1,
		Version:       "5.0",
		IPHash:        HashIP("10.0.0.1"),
		UserAgentHash: HashUserAgent("test"),
	}

	// Should not panic with window=0, and should still insert
	inserted, err := RecordInstall(ctx, database, params, 0)
	if err != nil {
		t.Fatalf("zero window: %v", err)
	}
	if !inserted {
		t.Error("expected insert with zero window (defaults to 3600)")
	}

	// Duplicate should still be caught (same default bucket)
	inserted, err = RecordInstall(ctx, database, params, -1)
	if err != nil {
		t.Fatalf("negative window: %v", err)
	}
	if inserted {
		t.Error("duplicate should be deduplicated even with negative window")
	}
}

func TestRecordInstall_DifferentVersionsNotDeduplicated(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	base := InstallParams{
		PackageID:     1,
		IPHash:        HashIP("10.0.0.1"),
		UserAgentHash: HashUserAgent("Composer/2.0"),
	}

	base.Version = "5.0"
	_, _ = RecordInstall(ctx, database, base, 3600)

	base.Version = "4.0"
	inserted, err := RecordInstall(ctx, database, base, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Error("different version should not be deduplicated")
	}

	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM install_events").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 events, got %d", count)
	}
}

func TestRecordInstall_DifferentPackagesNotDeduplicated(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	ipHash := HashIP("10.0.0.1")
	uaHash := HashUserAgent("Composer/2.0")

	_, _ = RecordInstall(ctx, database, InstallParams{PackageID: 1, Version: "5.0", IPHash: ipHash, UserAgentHash: uaHash}, 3600)
	inserted, _ := RecordInstall(ctx, database, InstallParams{PackageID: 2, Version: "5.0", IPHash: ipHash, UserAgentHash: uaHash}, 3600)

	if !inserted {
		t.Error("different package should not be deduplicated")
	}
}

func TestLookupPackageID(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	id, err := LookupPackageID(ctx, database, "wp-plugin/akismet")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero ID for active plugin")
	}

	id, _ = LookupPackageID(ctx, database, "wp-theme/astra")
	if id == 0 {
		t.Error("expected non-zero ID for active theme")
	}

	id, _ = LookupPackageID(ctx, database, "wp-plugin/nonexistent")
	if id != 0 {
		t.Error("expected 0 for unknown package")
	}

	id, _ = LookupPackageID(ctx, database, "invalid-vendor/foo")
	if id != 0 {
		t.Error("expected 0 for invalid vendor")
	}

	id, _ = LookupPackageID(ctx, database, "no-slash")
	if id != 0 {
		t.Error("expected 0 for invalid format")
	}
}

func TestAggregateInstalls(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour).Format(time.RFC3339)
	old := now.AddDate(0, 0, -400).Format(time.RFC3339) // 400 days ago, beyond retention

	// Insert events: 2 recent for akismet, 1 old for akismet, 1 recent for astra
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '5.0', 'a', 'b', 1, 'h1', ?)`, recent)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '4.0', 'a', 'b', 1, 'h2', ?)`, recent)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '3.0', 'a', 'b', 1, 'h3', ?)`, old)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (2, '4.0', 'c', 'd', 1, 'h4', ?)`, recent)

	result, err := AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if result.PackagesUpdated != 2 {
		t.Errorf("packages_updated = %d, want 2", result.PackagesUpdated)
	}

	// Old event (60 days ago) should be pruned (retention = 45 days)
	if result.EventsPruned != 1 {
		t.Errorf("events_pruned = %d, want 1", result.EventsPruned)
	}

	// Check akismet: total=3 (from monthly_installs), 30d=2 (from recent events)
	var total, recent30d int
	_ = database.QueryRow("SELECT wp_packages_installs_total, wp_packages_installs_30d FROM packages WHERE name='akismet'").Scan(&total, &recent30d)
	if total != 3 {
		t.Errorf("akismet total = %d, want 3", total)
	}
	if recent30d != 2 {
		t.Errorf("akismet 30d = %d, want 2", recent30d)
	}

	// Check astra: total=1, 30d=1
	_ = database.QueryRow("SELECT wp_packages_installs_total, wp_packages_installs_30d FROM packages WHERE name='astra'").Scan(&total, &recent30d)
	if total != 1 {
		t.Errorf("astra total = %d, want 1", total)
	}
	if recent30d != 1 {
		t.Errorf("astra 30d = %d, want 1", recent30d)
	}

	// Check last_installed_at is set
	var lastInstalled *string
	_ = database.QueryRow("SELECT last_installed_at FROM packages WHERE name='akismet'").Scan(&lastInstalled)
	if lastInstalled == nil {
		t.Error("last_installed_at should be set")
	}

	// Check monthly_installs: akismet should have 2 months (recent + 60d ago), astra 1 month
	var monthlyCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM monthly_installs WHERE package_id = 1").Scan(&monthlyCount)
	if monthlyCount != 2 {
		t.Errorf("akismet monthly rows = %d, want 2", monthlyCount)
	}
	_ = database.QueryRow("SELECT COUNT(*) FROM monthly_installs WHERE package_id = 2").Scan(&monthlyCount)
	if monthlyCount != 1 {
		t.Errorf("astra monthly rows = %d, want 1", monthlyCount)
	}

	// Verify counts per month for akismet
	var installs int
	recentMonth := now.Add(-24 * time.Hour).Format("2006-01")
	oldMonth := now.AddDate(0, 0, -400).Format("2006-01")
	_ = database.QueryRow("SELECT installs FROM monthly_installs WHERE package_id = 1 AND month = ?", recentMonth).Scan(&installs)
	if installs != 2 {
		t.Errorf("akismet %s installs = %d, want 2", recentMonth, installs)
	}
	_ = database.QueryRow("SELECT installs FROM monthly_installs WHERE package_id = 1 AND month = ?", oldMonth).Scan(&installs)
	if installs != 1 {
		t.Errorf("akismet %s installs = %d, want 1", oldMonth, installs)
	}

	// Verify watermark was saved
	var wm string
	_ = database.QueryRow("SELECT value FROM site_meta WHERE key = 'aggregate_watermark'").Scan(&wm)
	if wm == "" || wm == "0" {
		t.Error("watermark should be saved after aggregation")
	}
}

func TestAggregateInstalls_Incremental(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour).Format(time.RFC3339)

	// First batch: 2 events
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '5.0', 'a', 'b', 1, 'h1', ?)`, recent)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '4.0', 'a', 'b', 1, 'h2', ?)`, recent)

	_, err := AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("first aggregate: %v", err)
	}

	var total int
	_ = database.QueryRow("SELECT wp_packages_installs_total FROM packages WHERE name='akismet'").Scan(&total)
	if total != 2 {
		t.Errorf("after first run: total = %d, want 2", total)
	}

	// Second batch: 1 more event in the same month
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '3.0', 'a', 'b', 1, 'h3', ?)`, recent)

	_, err = AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("second aggregate: %v", err)
	}

	// Total should be 3, not 5 (no double-counting)
	_ = database.QueryRow("SELECT wp_packages_installs_total FROM packages WHERE name='akismet'").Scan(&total)
	if total != 3 {
		t.Errorf("after second run: total = %d, want 3", total)
	}

	// Monthly should show 3 for the recent month
	recentMonth := now.Add(-24 * time.Hour).Format("2006-01")
	var installs int
	_ = database.QueryRow("SELECT installs FROM monthly_installs WHERE package_id = 1 AND month = ?", recentMonth).Scan(&installs)
	if installs != 3 {
		t.Errorf("monthly installs = %d, want 3", installs)
	}
}

func TestAggregateInstalls_NoNewEvents(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	recent := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '5.0', 'a', 'b', 1, 'h1', ?)`, recent)

	// First run processes the event
	_, err := AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("first aggregate: %v", err)
	}

	// Second run with no new events should be a no-op for monthly
	_, err = AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("second aggregate: %v", err)
	}

	// Total should still be 1
	var total int
	_ = database.QueryRow("SELECT wp_packages_installs_total FROM packages WHERE name='akismet'").Scan(&total)
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestAggregateInstalls_Resets30d(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	// Set a stale 30d count with no recent events
	_, _ = database.Exec("UPDATE packages SET wp_packages_installs_30d = 50 WHERE name='akismet'")

	// Only old events
	old := time.Now().UTC().AddDate(0, 0, -400).Format(time.RFC3339)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '5.0', 'a', 'b', 1, 'h1', ?)`, old)

	result, err := AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if result.PackagesReset != 1 {
		t.Errorf("packages_reset = %d, want 1", result.PackagesReset)
	}

	var recent30d int
	_ = database.QueryRow("SELECT wp_packages_installs_30d FROM packages WHERE name='akismet'").Scan(&recent30d)
	if recent30d != 0 {
		t.Errorf("30d should be reset to 0, got %d", recent30d)
	}
}

func TestAggregateInstalls_PrunesOldEvents(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC()
	recent := now.Add(-24 * time.Hour).Format(time.RFC3339)
	old := now.AddDate(0, 0, -400).Format(time.RFC3339)

	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '5.0', 'a', 'b', 1, 'h1', ?)`, recent)
	_, _ = database.Exec(`INSERT INTO install_events (package_id, version, ip_hash, user_agent_hash, dedupe_bucket, dedupe_hash, created_at) VALUES (1, '4.0', 'a', 'b', 1, 'h2', ?)`, old)

	result, err := AggregateInstalls(ctx, database)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	if result.EventsPruned != 1 {
		t.Errorf("events_pruned = %d, want 1", result.EventsPruned)
	}

	// Old event should be gone
	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM install_events").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 remaining event, got %d", count)
	}

	// But monthly_installs should still reflect the old event's month
	var monthlyCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM monthly_installs WHERE package_id = 1").Scan(&monthlyCount)
	if monthlyCount != 2 {
		t.Errorf("expected 2 monthly rows (data preserved after pruning), got %d", monthlyCount)
	}
}
