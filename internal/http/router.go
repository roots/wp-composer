package http

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/roots/wp-composer/internal/app"
)

// cacheControl wraps an http.Handler and sets the Cache-Control header.
func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// hashPattern matches the content hash inserted by assetPath (e.g. ".a1b2c3d4e5f6").
var hashPattern = regexp.MustCompile(`\.[0-9a-f]{12}(\.[^.]+)$`)

// stripAssetHash removes the content hash from the URL path so the embedded
// file server can find the original file.
// e.g. "/assets/styles/app.a1b2c3d4e5f6.css" → "/assets/styles/app.css"
func stripAssetHash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = hashPattern.ReplaceAllString(r.URL.Path, "$1")
		next.ServeHTTP(w, r)
	})
}

func NewRouter(a *app.App) chi.Router {
	r := chi.NewRouter()
	tmpl := loadTemplates(a.Config.Env)

	r.Use(middleware.RequestID)
	if a.Config.Server.TrustProxy {
		r.Use(middleware.RealIP)
	}

	r.Use(middleware.Recoverer)

	sentryMiddleware := sentryhttp.New(sentryhttp.Options{Repanic: true})
	r.Use(sentryMiddleware.Handle)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	staticSub, _ := fs.Sub(staticFS, "static")
	staticServer := http.FileServer(http.FS(staticSub))
	cachedStatic := cacheControl("public, max-age=31536000, immutable", stripAssetHash(staticServer))
	for _, f := range []string{"/favicon.ico", "/icon.svg", "/icon-192.png", "/icon-512.png", "/apple-touch-icon.png", "/manifest.webmanifest"} {
		r.Get(f, cachedStatic.ServeHTTP)
	}
	r.Get("/assets/*", cachedStatic.ServeHTTP)

	// Ensure fallback OG image exists (uploads to R2 in production)
	ensureLocalFallbackOG(a.Config)

	// Serve OG images from local disk (dev mode — production uses CDN)
	if a.Config.R2.CDNPublicURL == "" {
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
		r.Post("/builds/trigger", handleTriggerBuild(a))
		r.Get("/logs", handleAdminLogs(tmpl))

		r.Get("/logs/stream", noTimeout(handleAdminLogStream(a)))
	})

	r.Mount("/admin", admin)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, tmpl.notFound, "layout", map[string]any{"Gone": false, "CDNURL": a.Config.R2.CDNPublicURL})
	})

	return r
}
