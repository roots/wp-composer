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

func setupClosedTestApp(t *testing.T) *app.App {
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
			permanently_closed INTEGER NOT NULL DEFAULT 0,
			UNIQUE(type, name)
		);
		INSERT INTO packages (type, name, permanently_closed) VALUES ('plugin', 'zeta-closed', 1);
		INSERT INTO packages (type, name, permanently_closed) VALUES ('plugin', 'alpha-closed', 1);
		INSERT INTO packages (type, name, permanently_closed) VALUES ('plugin', 'still-open', 0);
		INSERT INTO packages (type, name, permanently_closed) VALUES ('theme', 'old-theme', 1);
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

func TestAPIClosedPackages_Plugins(t *testing.T) {
	a := setupClosedTestApp(t)
	handler := handleAPIClosedPackages(a)

	req := httptest.NewRequest("GET", "/api/packages/wp-plugin/closed", nil)
	req.SetPathValue("type", "wp-plugin")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var slugs []string
	if err := json.NewDecoder(w.Body).Decode(&slugs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"alpha-closed", "zeta-closed"}
	if len(slugs) != len(want) {
		t.Fatalf("got %v, want %v", slugs, want)
	}
	for i, s := range want {
		if slugs[i] != s {
			t.Errorf("slug %d: got %q, want %q", i, slugs[i], s)
		}
	}
}

func TestAPIClosedPackages_Themes(t *testing.T) {
	a := setupClosedTestApp(t)
	handler := handleAPIClosedPackages(a)

	req := httptest.NewRequest("GET", "/api/packages/wp-theme/closed", nil)
	req.SetPathValue("type", "wp-theme")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var slugs []string
	if err := json.NewDecoder(w.Body).Decode(&slugs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(slugs) != 1 || slugs[0] != "old-theme" {
		t.Errorf("got %v, want [old-theme]", slugs)
	}
}

func TestAPIClosedPackages_BarePrefix(t *testing.T) {
	a := setupClosedTestApp(t)
	handler := handleAPIClosedPackages(a)

	req := httptest.NewRequest("GET", "/api/packages/plugin/closed", nil)
	req.SetPathValue("type", "plugin")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (bare type without wp- prefix)", w.Code)
	}
}

func TestAPIClosedPackages_UnknownType(t *testing.T) {
	a := setupClosedTestApp(t)
	handler := handleAPIClosedPackages(a)

	req := httptest.NewRequest("GET", "/api/packages/widget/closed", nil)
	req.SetPathValue("type", "widget")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for unknown type", w.Code)
	}
}

func TestAPIClosedPackages_EmptyResult(t *testing.T) {
	a := setupClosedTestApp(t)
	if _, err := a.DB.Exec(`DELETE FROM packages WHERE type = 'theme'`); err != nil {
		t.Fatal(err)
	}
	handler := handleAPIClosedPackages(a)

	req := httptest.NewRequest("GET", "/api/packages/wp-theme/closed", nil)
	req.SetPathValue("type", "wp-theme")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	// Must be `[]`, not `null`, so consumers can iterate without a nil check.
	if body := w.Body.String(); body != "[]\n" {
		t.Errorf("body: got %q, want %q", body, "[]\n")
	}
}
