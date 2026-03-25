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
	"github.com/roots/wp-packages/internal/telemetry"
)

func setupMonthlyTestApp(t *testing.T) *app.App {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(`
		CREATE TABLE packages (
			id INTEGER PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			UNIQUE(type, name)
		);
		CREATE TABLE monthly_installs (
			package_id INTEGER NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
			month TEXT NOT NULL,
			installs INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (package_id, month)
		);
		INSERT INTO packages (id, type, name, is_active) VALUES (1, 'plugin', 'akismet', 1);
		INSERT INTO packages (id, type, name, is_active) VALUES (2, 'theme', 'developer', 1);
		INSERT INTO packages (id, type, name, is_active) VALUES (3, 'plugin', 'inactive-plugin', 0);
		INSERT INTO monthly_installs (package_id, month, installs) VALUES (1, '2026-03', 142);
		INSERT INTO monthly_installs (package_id, month, installs) VALUES (1, '2026-04', 88);
		INSERT INTO monthly_installs (package_id, month, installs) VALUES (2, '2026-03', 50);
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

func TestAPIMonthlyInstalls_ReturnsJSON(t *testing.T) {
	a := setupMonthlyTestApp(t)
	handler := handleAPIMonthlyInstalls(a)

	req := httptest.NewRequest("GET", "/api/packages/plugin/akismet/installs", nil)
	req.SetPathValue("type", "plugin")
	req.SetPathValue("name", "akismet")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var resp []telemetry.MonthlyInstall
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("got %d months, want 2", len(resp))
	}
	if resp[0].Month != "2026-03" || resp[0].Installs != 142 {
		t.Errorf("first month: got %+v, want {2026-03 142}", resp[0])
	}
	if resp[1].Month != "2026-04" || resp[1].Installs != 88 {
		t.Errorf("second month: got %+v, want {2026-04 88}", resp[1])
	}
}

func TestAPIMonthlyInstalls_NotFound(t *testing.T) {
	a := setupMonthlyTestApp(t)
	handler := handleAPIMonthlyInstalls(a)

	req := httptest.NewRequest("GET", "/api/packages/plugin/nonexistent/installs", nil)
	req.SetPathValue("type", "plugin")
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestAPIMonthlyInstalls_InactivePackage(t *testing.T) {
	a := setupMonthlyTestApp(t)
	handler := handleAPIMonthlyInstalls(a)

	req := httptest.NewRequest("GET", "/api/packages/plugin/inactive-plugin/installs", nil)
	req.SetPathValue("type", "plugin")
	req.SetPathValue("name", "inactive-plugin")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for inactive package", w.Code)
	}
}

func TestAPIMonthlyInstalls_EmptyResult(t *testing.T) {
	a := setupMonthlyTestApp(t)
	handler := handleAPIMonthlyInstalls(a)

	// theme/developer has data, but let's query a package with no monthly installs
	// by adding a new active package with no installs
	_, _ = a.DB.Exec(`INSERT INTO packages (id, type, name, is_active) VALUES (99, 'plugin', 'empty', 1)`)

	req := httptest.NewRequest("GET", "/api/packages/plugin/empty/installs", nil)
	req.SetPathValue("type", "plugin")
	req.SetPathValue("name", "empty")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp []telemetry.MonthlyInstall
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("got %d months, want 0", len(resp))
	}
}

func TestAPIMonthlyInstalls_StripsWPPrefix(t *testing.T) {
	a := setupMonthlyTestApp(t)
	handler := handleAPIMonthlyInstalls(a)

	req := httptest.NewRequest("GET", "/api/packages/wp-theme/developer/installs", nil)
	req.SetPathValue("type", "wp-theme")
	req.SetPathValue("name", "developer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (wp- prefix should be stripped)", w.Code)
	}

	var resp []telemetry.MonthlyInstall
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp) != 1 || resp[0].Installs != 50 {
		t.Errorf("got %+v, want [{2026-03 50}]", resp)
	}
}
