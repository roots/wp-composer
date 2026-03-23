package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/config"
	"github.com/roots/wp-packages/internal/deploy"
	"github.com/roots/wp-packages/internal/og"
	"github.com/roots/wp-packages/internal/version"
)

const perPage = 12

// captureError reports a non-panic error to Sentry with the request's hub.
// It silently ignores context cancellation errors (timeouts, client disconnects)
// since these are expected during normal operation.
func captureError(r *http.Request, err error) {
	if r.Context().Err() != nil {
		return
	}
	if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
		hub.CaptureException(err)
	} else {
		sentry.CaptureException(err)
	}
}

// setETag computes an ETag from the given seed strings, sets it on the
// response, and returns true if the client already has this version (304).
func setETag(w http.ResponseWriter, r *http.Request, parts ...string) bool {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

type packageRow struct {
	Type                    string
	Name                    string
	DisplayName             string
	Description             string
	Author                  string
	Homepage                string
	CurrentVersion          string
	Downloads               int64
	ActiveInstalls          int64
	IsActive                bool
	LastSyncedAt            string
	WpPackagesInstallsTotal int64
}

type versionRow struct {
	Version  string
	IsLatest bool
}

func handleIndex(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := publicFilters{
			Search: r.URL.Query().Get("search"),
			Type:   r.URL.Query().Get("type"),
			Sort:   r.URL.Query().Get("sort"),
		}
		if filters.Sort == "" {
			filters.Sort = "composer_installs"
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		packages, total, err := queryPackages(r.Context(), a.DB, filters, page, perPage)
		if err != nil {
			a.Logger.Error("querying packages", "error", err)
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + perPage - 1) / perPage

		stats := queryIndexStats(r.Context(), a.DB)
		stats.RootsDownloads = a.Packagist.Total()

		searchURL := a.Config.AppURL + "/?search={search_term_string}"
		jsonLDData := map[string]any{
			"@context": "https://schema.org",
			"@type":    "WebSite",
			"name":     "WP Packages",
			"url":      a.Config.AppURL + "/",
			"potentialAction": map[string]any{
				"@type":       "SearchAction",
				"target":      searchURL,
				"query-input": "required name=search_term_string",
			},
		}

		w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=300, stale-while-revalidate=3600")
		if setETag(w, r, "index", stats.UpdatedAt, filters.Search, filters.Type, filters.Sort, strconv.Itoa(page)) {
			return
		}

		render(w, r, tmpl.index, "layout", map[string]any{
			"Packages":   packages,
			"Filters":    filters,
			"Page":       page,
			"Total":      total,
			"TotalPages": totalPages,
			"Stats":      stats,
			"AppURL":     a.Config.AppURL,
			"CDNURL":     a.Config.R2.CDNPublicURL,
			"OGImage":    ogImageURL(a.Config, "social/default.png"),
			"JSONLD":     jsonLDData,
		})
	}
}

func handleIndexPartial(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := publicFilters{
			Search: r.URL.Query().Get("search"),
			Type:   r.URL.Query().Get("type"),
			Sort:   r.URL.Query().Get("sort"),
		}
		if filters.Sort == "" {
			filters.Sort = "composer_installs"
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		packages, total, err := queryPackages(r.Context(), a.DB, filters, page, perPage)
		if err != nil {
			a.Logger.Error("querying packages", "error", err)
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + perPage - 1) / perPage

		w.Header().Set("X-Robots-Tag", "noindex")
		render(w, r, tmpl.indexPartial, "package-results", map[string]any{
			"Packages":   packages,
			"Filters":    filters,
			"Page":       page,
			"Total":      total,
			"TotalPages": totalPages,
		})
	}
}

func handleCompare(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
		render(w, r, tmpl.compare, "layout", map[string]any{
			"AppURL":  a.Config.AppURL,
			"CDNURL":  a.Config.R2.CDNPublicURL,
			"OGImage": ogImageURL(a.Config, "social/default.png"),
		})
	}
}

func handleRootsWordpress(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
		render(w, r, tmpl.rootsWordpress, "layout", map[string]any{
			"AppURL":         a.Config.AppURL,
			"CDNURL":         a.Config.R2.CDNPublicURL,
			"OGImage":        ogImageURL(a.Config, "social/default.png"),
			"RootsDownloads": a.Packagist.Total(),
		})
	}
}

func handleDetail(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType := r.PathValue("type")
		name := r.PathValue("name")

		// Strip wp- prefix from type
		pkgType = strings.TrimPrefix(pkgType, "wp-")

		pkg, err := queryPackageDetail(r.Context(), a.DB, pkgType, name)
		if err != nil {
			gone := packageExistsInactive(r.Context(), a.DB, pkgType, name)
			if gone {
				w.WriteHeader(http.StatusGone)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
			render(w, r, tmpl.notFound, "layout", map[string]any{"Gone": gone, "CDNURL": a.Config.R2.CDNPublicURL})
			return
		}

		versions := parseVersions(pkg)

		var ogImage string
		if pkg.OGImageGeneratedAt != nil {
			ogImage = ogImageURL(a.Config, "social/"+pkg.Type+"/"+pkg.Name+".png")
		}

		displayName := pkg.Name
		if pkg.DisplayName != "" {
			displayName = pkg.DisplayName
		}
		pkgURL := a.Config.AppURL + "/packages/wp-" + pkg.Type + "/" + pkg.Name

		softwareApp := map[string]any{
			"@context":            "https://schema.org",
			"@type":               "SoftwareApplication",
			"name":                displayName,
			"applicationCategory": "WordPress " + pkg.Type,
			"operatingSystem":     "WordPress",
			"url":                 pkgURL,
		}
		if pkg.CurrentVersion != "" {
			softwareApp["softwareVersion"] = pkg.CurrentVersion
		}
		if pkg.Author != "" {
			softwareApp["author"] = map[string]any{
				"@type": "Person",
				"name":  pkg.Author,
			}
		}
		if pkg.ActiveInstalls > 0 {
			softwareApp["interactionStatistic"] = map[string]any{
				"@type":                "InteractionCounter",
				"interactionType":      "https://schema.org/InstallAction",
				"userInteractionCount": pkg.ActiveInstalls,
			}
		}
		if pkg.Description != "" {
			softwareApp["description"] = pkg.Description
		}

		breadcrumbs := map[string]any{
			"@context": "https://schema.org",
			"@type":    "BreadcrumbList",
			"itemListElement": []map[string]any{
				{
					"@type":    "ListItem",
					"position": 1,
					"name":     "Packages",
					"item":     a.Config.AppURL + "/",
				},
				{
					"@type":    "ListItem",
					"position": 2,
					"name":     displayName,
					"item":     pkgURL,
				},
			},
		}

		w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=3600, stale-while-revalidate=86400")
		if setETag(w, r, "detail", pkg.Type, pkg.Name, pkg.UpdatedAt) {
			return
		}

		render(w, r, tmpl.detail, "layout", map[string]any{
			"Package":  pkg,
			"Versions": versions,
			"AppURL":   a.Config.AppURL,
			"CDNURL":   a.Config.R2.CDNPublicURL,
			"OGImage":  ogImage,
			"JSONLD":   []any{softwareApp, breadcrumbs},
		})
	}
}

func handleAdminDashboard(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := queryDashboardStats(r.Context(), a.DB)

		// Get current build — set it on the Stats struct
		currentBuild, _ := deploy.CurrentBuildID("storage/repository")
		s := stats["Stats"].(struct {
			TotalPackages  int64
			ActivePlugins  int64
			ActiveThemes   int64
			TotalInstalls  int64
			Installs30d    int64
			CurrentBuild   string
			StatsUpdatedAt string
		})
		s.CurrentBuild = currentBuild
		stats["Stats"] = s

		render(w, r, tmpl.adminDashboard, "admin_layout", stats)
	}
}

func handleAdminPackages(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filters := adminFilters{
			Search: r.URL.Query().Get("search"),
			Type:   r.URL.Query().Get("type"),
			Active: r.URL.Query().Get("active"),
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		packages, total, err := queryAdminPackages(r.Context(), a.DB, filters, page, 50)
		if err != nil {
			a.Logger.Error("querying admin packages", "error", err)
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + 50 - 1) / 50

		render(w, r, tmpl.adminPackages, "admin_layout", map[string]any{
			"Packages":   packages,
			"Filters":    filters,
			"Page":       page,
			"Total":      total,
			"TotalPages": totalPages,
		})
	}
}

func handleAdminBuilds(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		builds, err := queryBuilds(r.Context(), a.DB)
		if err != nil {
			a.Logger.Error("querying builds", "error", err)
			captureError(r, err)
		}

		currentID, _ := deploy.CurrentBuildID("storage/repository")
		for i := range builds {
			if builds[i].ID == currentID {
				builds[i].IsCurrent = true
			}
		}

		data := map[string]any{
			"Builds":    builds,
			"Triggered": r.URL.Query().Get("triggered") == "1",
			"Error":     r.URL.Query().Get("error"),
		}
		if len(builds) > 0 {
			data["LastBuildStartedAt"] = builds[0].StartedAt
		}

		render(w, r, tmpl.adminBuilds, "admin_layout", data)
	}
}

func handleTriggerBuild(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		self, err := os.Executable()
		if err != nil {
			a.Logger.Error("failed to find executable", "error", err)
			http.Redirect(w, r, "/admin/builds?error=internal+error", http.StatusSeeOther)
			return
		}

		// Clean up stale "running" rows (dead PID) before checking.
		markStaleBuildsCancelled(r.Context(), a.DB, a.Logger)

		// Atomically claim a build slot before starting the child. The row
		// is inserted with the server's own PID so that stale cleanup
		// (which checks PID liveness) cannot cancel it while we start the
		// child. The child will UPDATE the PID to its own once it begins.
		buildID := time.Now().UTC().Format("20060102-150405")
		res, err := a.DB.ExecContext(r.Context(), `
			INSERT INTO builds (id, started_at, status, pid,
				packages_total, packages_changed, packages_skipped,
				artifact_count, root_hash, manifest_json)
			SELECT ?, ?, 'running', ?, 0, 0, 0, 0, '', '{}'
			WHERE NOT EXISTS (
				SELECT 1 FROM builds WHERE status = 'running'
			)`,
			buildID,
			time.Now().UTC().Format(time.RFC3339),
			os.Getpid(),
		)
		if err != nil {
			a.Logger.Error("failed to claim build slot", "error", err)
			http.Redirect(w, r, "/admin/builds?error=internal+error", http.StatusSeeOther)
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			http.Redirect(w, r, "/admin/builds?error=build+already+running", http.StatusSeeOther)
			return
		}

		// Slot claimed — start the child. On failure, mark the row
		// failed so the slot is freed.
		cmd := exec.Command(self, "pipeline", "--build-id", buildID)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			a.Logger.Error("failed to start pipeline", "error", err)
			_, _ = a.DB.ExecContext(r.Context(), `
				UPDATE builds SET status = 'failed', finished_at = ?,
					error_message = ? WHERE id = ?`,
				time.Now().UTC().Format(time.RFC3339),
				"failed to start: "+err.Error(), buildID)
			http.Redirect(w, r, "/admin/builds?error=internal+error", http.StatusSeeOther)
			return
		}

		// Update the row with the child's actual PID so stale cleanup
		// tracks the right process going forward.
		if _, err := a.DB.ExecContext(r.Context(),
			`UPDATE builds SET pid = ? WHERE id = ?`,
			cmd.Process.Pid, buildID); err != nil {
			a.Logger.Warn("failed to update build PID", "build_id", buildID, "error", err)
		}

		// Reap the child in the background. If the child exits with an
		// error and the row is still in "running" state (i.e. the child
		// never got far enough to record its own outcome), mark it failed
		// here so the slot is freed.
		go func() {
			if err := cmd.Wait(); err != nil {
				a.Logger.Error("triggered pipeline failed", "error", err)
				now := time.Now().UTC().Format(time.RFC3339)
				_, _ = a.DB.ExecContext(context.Background(), `
					UPDATE builds SET status = 'failed', finished_at = ?,
						error_message = ? WHERE id = ? AND status = 'running'`,
					now, err.Error(), buildID)
			} else {
				a.Logger.Info("triggered pipeline completed")
			}
		}()

		http.Redirect(w, r, "/admin/builds?triggered=1", http.StatusSeeOther)
	}
}

// markStaleBuildsCancelled finds builds with status "running" whose PID is no
// longer alive and marks them as "cancelled".
func markStaleBuildsCancelled(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, pid FROM builds WHERE status = 'running' AND pid IS NOT NULL`)
	if err != nil {
		return
	}

	var staleIDs []string
	for rows.Next() {
		var id string
		var pid int
		if err := rows.Scan(&id, &pid); err != nil {
			continue
		}
		if err := syscall.Kill(pid, 0); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				staleIDs = append(staleIDs, id)
			} else {
				logger.Warn("stale build check: unexpected kill(0) error", "build_id", id, "pid", pid, "error", err)
			}
		}
	}
	_ = rows.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range staleIDs {
		_, _ = db.ExecContext(ctx, `UPDATE builds SET status = 'cancelled', finished_at = ? WHERE id = ?`, now, id)
	}
}

var logFiles = map[string]string{
	"wppackages": filepath.Join("storage", "logs", "wppackages.log"),
	"pipeline":   filepath.Join("storage", "logs", "pipeline.log"),
}

func handleAdminLogs(tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, tmpl.adminLogs, "admin_layout", nil)
	}
}

func handleAdminLogStream(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Query().Get("file")
		logPath, ok := logFiles[file]
		if !ok {
			http.Error(w, "unknown log file", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ctx := r.Context()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// Wait for the file to exist
		var f *os.File
		for f == nil {
			if opened, err := os.Open(logPath); err == nil {
				f = opened
			} else {
				_, _ = fmt.Fprintf(w, "data: Waiting for %s ...\n\n", filepath.Base(logPath))
				flusher.Flush()
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
		defer func() { _ = f.Close() }()

		// Send initial batch: last 200 lines
		lines := tailFile(logPath, 200)
		for _, line := range lines {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
		}
		flusher.Flush()

		// Seek to end for tailing
		offset, _ := f.Seek(0, 2)

		buf := make([]byte, 64*1024)
		var partial string
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(logPath)
				if err != nil {
					continue
				}
				if info.Size() < offset {
					// File was truncated/rotated, reopen
					_ = f.Close()
					f, err = os.Open(logPath)
					if err != nil {
						continue
					}
					offset = 0
				}
				if info.Size() == offset {
					continue
				}
				_, _ = f.Seek(offset, 0)
				n, err := f.Read(buf)
				if n > 0 {
					offset += int64(n)
					chunk := partial + string(buf[:n])
					partial = ""
					newLines := strings.Split(chunk, "\n")
					if !strings.HasSuffix(chunk, "\n") {
						partial = newLines[len(newLines)-1]
						newLines = newLines[:len(newLines)-1]
					}
					for _, line := range newLines {
						if line != "" {
							if _, werr := fmt.Fprintf(w, "data: %s\n\n", line); werr != nil {
								return
							}
						}
					}
					flusher.Flush()
				}
				if err != nil {
					continue
				}
			}
		}
	}
}

func tailFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ogImageURL returns the full OG image URL for a given key.
// In production, it uses the CDN. In local dev, it uses /og/ routes.
func ogImageURL(cfg *config.Config, key string) string {
	if cfg.R2.CDNPublicURL != "" {
		return cfg.R2.CDNPublicURL + "/" + key
	}
	if cfg.AppURL != "" {
		return cfg.AppURL + "/og/" + key
	}
	return "/og/" + key
}

// handleOGImage serves OG images from local disk (dev mode).
func handleOGImage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/og/")
		clean := filepath.Clean(key)
		if strings.Contains(clean, "..") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join("storage", "og", clean)
		data, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(data)
	}
}

// ensureLocalFallbackOG generates the fallback OG image to disk and uploads to R2 if configured.
func ensureLocalFallbackOG(cfg *config.Config) {
	path := "storage/og/social/default.png"

	data, err := og.GenerateFallbackImage()
	if err != nil {
		return
	}

	// Always write locally
	_ = os.MkdirAll("storage/og/social", 0o755)
	_ = os.WriteFile(path, data, 0o644)

	// Upload to R2 CDN if configured
	uploader := og.NewUploader(cfg.R2)
	if uploader.IsR2() {
		_ = uploader.Upload(context.Background(), "social/default.png", data)
	}
}

// Query helpers

type indexStats struct {
	PluginInstalls int64
	ThemeInstalls  int64
	RootsDownloads int64
	UpdatedAt      string
}

func queryIndexStats(ctx context.Context, db *sql.DB) indexStats {
	var s indexStats
	_ = db.QueryRowContext(ctx, "SELECT plugin_installs, theme_installs, COALESCE(updated_at,'') FROM package_stats WHERE id = 1").Scan(&s.PluginInstalls, &s.ThemeInstalls, &s.UpdatedAt)
	return s
}

// collapseSlug strips hyphens, underscores, and spaces to produce a
// compact form suitable for LIKE-matching against similarly collapsed names.
func collapseSlug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// ftsQuery converts a user search string into an FTS5 query.
// Each token becomes a prefix search, joined with AND.
// e.g. "woo commerce" -> "woo* AND commerce*"
func ftsQuery(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		// Escape double quotes to prevent FTS5 syntax injection
		w = strings.ReplaceAll(w, `"`, `""`)
		words[i] = `"` + w + `"` + "*"
	}
	return strings.Join(words, " AND ")
}

func queryPackages(ctx context.Context, db *sql.DB, f publicFilters, page, limit int) ([]packageRow, int, error) {
	where := "is_active = 1"
	args := []any{}

	if q := ftsQuery(f.Search); q != "" {
		where += " AND (id IN (SELECT rowid FROM packages_fts WHERE packages_fts MATCH ?) OR REPLACE(name, '-', '') LIKE ?)"
		args = append(args, q, "%"+collapseSlug(f.Search)+"%")
	}
	if f.Type != "" {
		where += " AND type = ?"
		args = append(args, f.Type)
	}

	orderBy := "wp_packages_installs_total DESC"
	switch f.Sort {
	case "active_installs":
		orderBy = "active_installs DESC"
	case "updated":
		orderBy = "last_committed DESC NULLS LAST"
	case "name":
		orderBy = "name ASC"
	}

	var total int
	if f.Search == "" && f.Type == "" {
		_ = db.QueryRowContext(ctx, "SELECT active_plugins + active_themes FROM package_stats WHERE id = 1").Scan(&total)
	} else {
		countQ := "SELECT COUNT(*) FROM packages WHERE " + where
		if err := db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	offset := (page - 1) * limit
	q := fmt.Sprintf(`SELECT type, name, COALESCE(display_name,''), COALESCE(description,''),
		COALESCE(current_version,''), downloads, active_installs, wp_packages_installs_total
		FROM packages WHERE %s ORDER BY %s LIMIT ? OFFSET ?`, where, orderBy)

	rows, err := db.QueryContext(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var pkgs []packageRow
	for rows.Next() {
		var p packageRow
		if err := rows.Scan(&p.Type, &p.Name, &p.DisplayName, &p.Description, &p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpPackagesInstallsTotal); err != nil {
			return nil, 0, fmt.Errorf("scanning package row: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, total, rows.Err()
}

type packageDetail struct {
	packageRow
	VersionsJSON       string
	UpdatedAt          string
	OGImageGeneratedAt *string
}

func packageExistsInactive(ctx context.Context, db *sql.DB, pkgType, name string) bool {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM packages WHERE type = ? AND name = ? AND is_active = 0`, pkgType, name).Scan(&n)
	return err == nil
}

func queryPackageDetail(ctx context.Context, db *sql.DB, pkgType, name string) (*packageDetail, error) {
	var p packageDetail
	err := db.QueryRowContext(ctx, `SELECT type, name, COALESCE(display_name,''), COALESCE(description,''),
		COALESCE(author,''), COALESCE(homepage,''), COALESCE(current_version,''),
		downloads, active_installs, wp_packages_installs_total, versions_json,
		COALESCE(updated_at,''), og_image_generated_at
		FROM packages WHERE type = ? AND name = ? AND is_active = 1`, pkgType, name,
	).Scan(&p.Type, &p.Name, &p.DisplayName, &p.Description, &p.Author, &p.Homepage,
		&p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpPackagesInstallsTotal,
		&p.VersionsJSON, &p.UpdatedAt, &p.OGImageGeneratedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func parseVersions(pkg *packageDetail) []versionRow {
	var versions map[string]string
	_ = json.Unmarshal([]byte(pkg.VersionsJSON), &versions)

	var rows []versionRow
	for v := range versions {
		rows = append(rows, versionRow{
			Version:  v,
			IsLatest: v == pkg.CurrentVersion,
		})
	}

	// Sort by semver descending, with latest always first
	slices.SortFunc(rows, func(a, b versionRow) int {
		if a.IsLatest != b.IsLatest {
			if a.IsLatest {
				return -1
			}
			return 1
		}
		return version.Compare(b.Version, a.Version)
	})
	return rows
}

func queryDashboardStats(ctx context.Context, db *sql.DB) map[string]any {
	stats := map[string]any{
		"Stats": struct {
			TotalPackages  int64
			ActivePlugins  int64
			ActiveThemes   int64
			TotalInstalls  int64
			Installs30d    int64
			CurrentBuild   string
			StatsUpdatedAt string
		}{},
	}

	var s struct {
		TotalPackages  int64
		ActivePlugins  int64
		ActiveThemes   int64
		TotalInstalls  int64
		Installs30d    int64
		CurrentBuild   string
		StatsUpdatedAt string
	}

	_ = db.QueryRowContext(ctx, `SELECT active_plugins, active_themes, active_plugins + active_themes,
		plugin_installs + theme_installs, installs_30d, COALESCE(updated_at,'') FROM package_stats WHERE id = 1`).Scan(
		&s.ActivePlugins, &s.ActiveThemes, &s.TotalPackages, &s.TotalInstalls, &s.Installs30d, &s.StatsUpdatedAt)

	stats["Stats"] = s
	return stats
}

func queryAdminPackages(ctx context.Context, db *sql.DB, f adminFilters, page, limit int) ([]packageRow, int, error) {
	where := "1=1"
	args := []any{}

	if q := ftsQuery(f.Search); q != "" {
		where += " AND (id IN (SELECT rowid FROM packages_fts WHERE packages_fts MATCH ?) OR REPLACE(name, '-', '') LIKE ?)"
		args = append(args, q, "%"+collapseSlug(f.Search)+"%")
	}
	if f.Type != "" {
		where += " AND type = ?"
		args = append(args, f.Type)
	}
	switch f.Active {
	case "1":
		where += " AND is_active = 1"
	case "0":
		where += " AND is_active = 0"
	}

	var total int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM packages WHERE "+where, args...).Scan(&total)

	offset := (page - 1) * limit
	q := fmt.Sprintf(`SELECT type, name, COALESCE(display_name,''), COALESCE(current_version,''),
		downloads, active_installs, wp_packages_installs_total, is_active, COALESCE(last_synced_at,'')
		FROM packages WHERE %s ORDER BY downloads DESC LIMIT ? OFFSET ?`, where)

	rows, err := db.QueryContext(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var pkgs []packageRow
	for rows.Next() {
		var p packageRow
		var isActive int
		_ = rows.Scan(&p.Type, &p.Name, &p.DisplayName, &p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpPackagesInstallsTotal, &isActive, &p.LastSyncedAt)
		p.IsActive = isActive == 1
		pkgs = append(pkgs, p)
	}
	return pkgs, total, rows.Err()
}

type buildRow struct {
	ID              string
	StartedAt       string
	PackagesTotal   int
	PackagesChanged int
	ArtifactCount   int
	Status          string
	IsCurrent       bool
	R2SyncedAt      string
	ErrorMessage    string
	DurationSeconds *int
	DiscoverSeconds *int
	UpdateSeconds   *int
	BuildSeconds    *int
	DeploySeconds   *int
	R2UploadSeconds *int
}

func queryBuilds(ctx context.Context, db *sql.DB) ([]buildRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, started_at, packages_total, packages_changed,
		artifact_count, status, COALESCE(r2_synced_at, ''), COALESCE(error_message, ''),
		duration_seconds, discover_seconds, update_seconds, build_seconds, deploy_seconds, r2_upload_seconds
		FROM builds ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var builds []buildRow
	for rows.Next() {
		var b buildRow
		_ = rows.Scan(&b.ID, &b.StartedAt, &b.PackagesTotal, &b.PackagesChanged,
			&b.ArtifactCount, &b.Status, &b.R2SyncedAt, &b.ErrorMessage,
			&b.DurationSeconds, &b.DiscoverSeconds, &b.UpdateSeconds, &b.BuildSeconds, &b.DeploySeconds, &b.R2UploadSeconds)
		builds = append(builds, b)
	}
	return builds, rows.Err()
}
