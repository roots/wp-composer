package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/config"
	"github.com/roots/wp-packages/internal/db"
)

func setupStatsTestApp(t *testing.T) *app.App {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(`
		CREATE TABLE package_stats (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			active_plugins INTEGER NOT NULL DEFAULT 0,
			active_themes INTEGER NOT NULL DEFAULT 0,
			plugin_installs INTEGER NOT NULL DEFAULT 0,
			theme_installs INTEGER NOT NULL DEFAULT 0,
			installs_30d INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
		INSERT INTO package_stats (id, active_plugins, active_themes, plugin_installs, theme_installs, installs_30d, updated_at)
		VALUES (1, 500, 200, 100000, 23456, 7890, datetime('now'));
	`)
	if err != nil {
		t.Fatal(err)
	}

	return &app.App{
		Config: &config.Config{},
		DB:     database,
		Logger: slog.Default(),
	}
}

func TestAPIStats_ReturnsJSON(t *testing.T) {
	a := setupStatsTestApp(t)
	handler := handleAPIStats(a)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats: got %d, want 200", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("Cache-Control: got %q, want public, max-age=300", cc)
	}

	var resp statsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.TotalInstalls != 123456 {
		t.Errorf("TotalInstalls: got %d, want 123456", resp.TotalInstalls)
	}
	if resp.Installs30d != 7890 {
		t.Errorf("Installs30d: got %d, want 7890", resp.Installs30d)
	}
	if resp.ActivePlugins != 500 {
		t.Errorf("ActivePlugins: got %d, want 500", resp.ActivePlugins)
	}
	if resp.ActiveThemes != 200 {
		t.Errorf("ActiveThemes: got %d, want 200", resp.ActiveThemes)
	}
	if resp.TotalPackages != 700 {
		t.Errorf("TotalPackages: got %d, want 700", resp.TotalPackages)
	}
}

func TestAPIStats_NoStats(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Create table but no rows
	_, _ = database.Exec(`
		CREATE TABLE package_stats (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			active_plugins INTEGER NOT NULL DEFAULT 0,
			active_themes INTEGER NOT NULL DEFAULT 0,
			plugin_installs INTEGER NOT NULL DEFAULT 0,
			theme_installs INTEGER NOT NULL DEFAULT 0,
			installs_30d INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
	`)

	a := &app.App{
		Config: &config.Config{},
		DB:     database,
		Logger: slog.Default(),
	}

	handler := handleAPIStats(a)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("GET /api/stats (no data): got %d, want 500", w.Code)
	}
}
