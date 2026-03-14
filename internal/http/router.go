package http

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/roots/wp-composer/internal/app"
)

func NewRouter(a *app.App) chi.Router {
	r := chi.NewRouter()
	tmpl := loadTemplates(a.Config.Env)

	r.Use(middleware.RequestID)
	if a.Config.Server.TrustProxy {
		r.Use(middleware.RealIP)
	}

	sentryMiddleware := sentryhttp.New(sentryhttp.Options{Repanic: true})
	r.Use(sentryMiddleware.Handle)

	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	staticSub, _ := fs.Sub(staticFS, "static")
	staticServer := http.FileServer(http.FS(staticSub))
	for _, f := range []string{"/favicon.ico", "/icon.svg", "/icon-192.png", "/icon-512.png", "/apple-touch-icon.png", "/manifest.webmanifest"} {
		r.Get(f, staticServer.ServeHTTP)
	}

	// Serve OG images from local disk (dev mode — production uses CDN)
	if a.Config.R2.CDNPublicURL == "" {
		ensureLocalFallbackOG()
		r.Get("/og/*", handleOGImage())
	}

	r.Get("/feed.xml", handleFeed(a))
	r.Get("/robots.txt", handleRobotsTxt(a))
	sitemaps := &sitemapData{}
	r.Get("/sitemap.xml", handleSitemapIndex(a, sitemaps))
	r.Get("/sitemap-pages.xml", handleSitemapPages(a, sitemaps))
	r.Get("/sitemap-packages-{page}.xml", handleSitemapPackages(a, sitemaps))

	r.Get("/", handleIndex(a, tmpl))
	r.Get("/packages-partial", handleIndexPartial(a, tmpl))
	r.Get("/packages/{type}/{name}", handleDetail(a, tmpl))
	r.Get("/wp-composer-vs-wpackagist", handleCompare(a, tmpl))
	r.Get("/roots-wordpress", handleRootsWordpress(a, tmpl))

	r.Post("/downloads", handleDownloads(a))

	// Serve static repository files from current build (local/dev mode)
	repoRoot := filepath.Join("storage", "repository", "current")
	if _, err := os.Stat(repoRoot); err == nil {
		fileServer := http.FileServer(http.Dir(repoRoot))
		r.Get("/packages.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fileServer.ServeHTTP(w, r)
		})
		r.Handle("/p/*", fileServer)
		r.Handle("/p2/*", fileServer)
	}

	// Admin subrouter — network-restricted, login is public within that, rest requires auth
	admin := chi.NewRouter()
	admin.Use(RequireAllowedIP(a.Config.Server.AdminAllowCIDR, a.Logger))

	admin.Get("/login", handleLoginPage(a))
	admin.Post("/login", handleLogin(a))
	admin.Post("/logout", handleLogout(a))

	admin.Group(func(r chi.Router) {
		r.Use(SessionAuth(a.DB))
		r.Use(RequireAdmin)

		r.Get("/", handleAdminDashboard(a, tmpl))
		r.Get("/packages", handleAdminPackages(a, tmpl))
		r.Get("/builds", handleAdminBuilds(a, tmpl))
	})

	r.Mount("/admin", admin)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		render(w, tmpl.notFound, "layout", map[string]any{"Gone": false, "CDNURL": a.Config.R2.CDNPublicURL})
	})

	return r
}
