package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/roots/wp-composer/internal/app"
	"github.com/roots/wp-composer/internal/config"
	"github.com/roots/wp-composer/internal/db"
	"github.com/roots/wp-composer/internal/packagist"
)

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	return &app.App{
		Config:    &config.Config{},
		DB:        database,
		Logger:    slog.Default(),
		Packagist: packagist.NewDownloadsCache(slog.Default()),
	}
}

// TestNewRouter_NoPanic verifies that all ServeMux patterns are valid and
// route registration does not panic.
func TestNewRouter_NoPanic(t *testing.T) {
	a := newTestApp(t)
	handler := NewRouter(a)
	if handler == nil {
		t.Fatal("NewRouter returned nil")
	}
}

func TestRouter_MethodNotAllowed(t *testing.T) {
	a := newTestApp(t)
	handler := NewRouter(a)

	// POST /health should return 405, not 404
	req := httptest.NewRequest("POST", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /health: got %d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow == "" {
		t.Error("POST /health: missing Allow header")
	}
}

func TestRouter_NotFound(t *testing.T) {
	a := newTestApp(t)
	handler := NewRouter(a)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /nonexistent: got %d, want 404", w.Code)
	}
}

func TestRouter_HandlerGenerated404PreservesBody(t *testing.T) {
	// A registered handler that returns 404 with its own body should not
	// have that body replaced by the custom not-found template.
	mux := http.NewServeMux()
	mux.Handle("GET /pkg/{name}", routeMarker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "package not found", http.StatusNotFound)
	})))

	a := newTestApp(t)
	tmpl := loadTemplates("")
	handler := appHandler(mux, tmpl, a, nil)

	req := httptest.NewRequest("GET", "/pkg/nope", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /pkg/nope: got %d, want 404", w.Code)
	}
	body := w.Body.String()
	if body != "package not found\n" {
		t.Errorf("handler-generated 404 body was replaced: got %q", body)
	}
}

func TestRouter_UnmatchedRouteRendersTemplate(t *testing.T) {
	// An unmatched route should render the custom 404 template, not the
	// default "404 page not found" text from ServeMux.
	mux := http.NewServeMux()
	mux.Handle("GET /health", routeMarker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})))

	a := newTestApp(t)
	tmpl := loadTemplates("")
	handler := appHandler(mux, tmpl, a, nil)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /nonexistent: got %d, want 404", w.Code)
	}
	body := w.Body.String()
	if body == "404 page not found\n" {
		t.Error("unmatched route got default ServeMux 404 body instead of custom template")
	}
}

func TestRouter_SitemapPackagesRoutes(t *testing.T) {
	a := newTestApp(t)
	handler := NewRouter(a)

	// Should not 404 — the route should be matched even though
	// it can't be a ServeMux pattern.
	req := httptest.NewRequest("GET", "/sitemap-packages-0.xml", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// 200 (if sitemap data generates) or 500 (no DB tables) are both acceptable;
	// 404 means the route wasn't matched.
	if w.Code == http.StatusNotFound {
		t.Error("GET /sitemap-packages-0.xml returned 404 — route not matched")
	}
}
