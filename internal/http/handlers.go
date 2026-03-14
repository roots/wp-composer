package http

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/roots/wp-composer/internal/app"
	"github.com/roots/wp-composer/internal/config"
	"github.com/roots/wp-composer/internal/deploy"
	"github.com/roots/wp-composer/internal/og"
)

const perPage = 24

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
	WpComposerInstallsTotal int64
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
			filters.Sort = "downloads"
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		packages, total, err := queryPackages(r.Context(), a.DB, filters, page, perPage)
		if err != nil {
			a.Logger.Error("querying packages", "error", err)
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
			"name":     "WP Composer",
			"url":      a.Config.AppURL + "/",
			"potentialAction": map[string]any{
				"@type":       "SearchAction",
				"target":      searchURL,
				"query-input": "required name=search_term_string",
			},
		}

		render(w, tmpl.index, "layout", map[string]any{
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
			filters.Sort = "downloads"
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}

		packages, total, err := queryPackages(r.Context(), a.DB, filters, page, perPage)
		if err != nil {
			a.Logger.Error("querying packages", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + perPage - 1) / perPage

		w.Header().Set("X-Robots-Tag", "noindex")
		render(w, tmpl.indexPartial, "package-results", map[string]any{
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
		render(w, tmpl.compare, "layout", map[string]any{
			"AppURL":  a.Config.AppURL,
			"CDNURL":  a.Config.R2.CDNPublicURL,
			"OGImage": ogImageURL(a.Config, "social/default.png"),
		})
	}
}

func handleRootsWordpress(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, tmpl.rootsWordpress, "layout", map[string]any{
			"AppURL":         a.Config.AppURL,
			"CDNURL":         a.Config.R2.CDNPublicURL,
			"OGImage":        ogImageURL(a.Config, "social/default.png"),
			"RootsDownloads": a.Packagist.Total(),
		})
	}
}

func handleDetail(a *app.App, tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType := chi.URLParam(r, "type")
		name := chi.URLParam(r, "name")

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
			render(w, tmpl.notFound, "layout", map[string]any{"Gone": gone, "CDNURL": a.Config.R2.CDNPublicURL})
			return
		}

		versions := parseVersions(pkg)

		ogKey := "social/" + pkg.Type + "/" + pkg.Name + ".png"
		if pkg.OGImageGeneratedAt == nil {
			// Generate on demand in background
			go generatePackageOG(a, pkg)
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

		render(w, tmpl.detail, "layout", map[string]any{
			"Package":  pkg,
			"Versions": versions,
			"AppURL":   a.Config.AppURL,
			"CDNURL":   a.Config.R2.CDNPublicURL,
			"OGImage":  ogImageURL(a.Config, ogKey),
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
			TotalPackages int64
			ActivePlugins int64
			ActiveThemes  int64
			TotalInstalls int64
			Installs30d   int64
			CurrentBuild  string
		})
		s.CurrentBuild = currentBuild
		stats["Stats"] = s

		render(w, tmpl.adminDashboard, "admin_layout", stats)
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
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		totalPages := (total + 50 - 1) / 50

		render(w, tmpl.adminPackages, "admin_layout", map[string]any{
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
		}

		currentID, _ := deploy.CurrentBuildID("storage/repository")
		for i := range builds {
			if builds[i].ID == currentID {
				builds[i].IsCurrent = true
			}
		}

		render(w, tmpl.adminBuilds, "admin_layout", map[string]any{
			"Builds": builds,
		})
	}
}

var logFiles = map[string]string{
	"wpcomposer": filepath.Join("storage", "logs", "wpcomposer.log"),
	"pipeline":   filepath.Join("storage", "logs", "pipeline.log"),
}

func handleAdminLogs(tmpl *templateSet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, tmpl.adminLogs, "admin_layout", nil)
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
				fmt.Fprintf(w, "data: Waiting for %s ...\n\n", filepath.Base(logPath))
				flusher.Flush()
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
		defer f.Close()

		// Send initial batch: last 200 lines
		lines := tailFile(logPath, 200)
		for _, line := range lines {
			fmt.Fprintf(w, "data: %s\n\n", line)
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
					f.Close()
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
		key := chi.URLParam(r, "*")
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

// generatePackageOG generates an OG image for a package and saves it.
func generatePackageOG(a *app.App, pkg *packageDetail) {
	data := og.PackageData{
		DisplayName:        pkg.DisplayName,
		Name:               pkg.Name,
		Type:               pkg.Type,
		CurrentVersion:     pkg.CurrentVersion,
		Description:        pkg.Description,
		ActiveInstalls:     og.FormatInstalls(pkg.ActiveInstalls),
		WpComposerInstalls: og.FormatInstalls(pkg.WpComposerInstallsTotal),
	}

	pngBytes, err := og.GeneratePackageImage(data)
	if err != nil {
		a.Logger.Error("generating OG image", "package", pkg.Name, "error", err)
		return
	}

	uploader := og.NewUploader(a.Config.R2)
	key := "social/" + pkg.Type + "/" + pkg.Name + ".png"
	if err := uploader.Upload(context.Background(), key, pngBytes); err != nil {
		a.Logger.Error("uploading OG image", "package", pkg.Name, "error", err)
		return
	}

	_ = og.MarkOGGeneratedBySlug(context.Background(), a.DB, pkg.Type, pkg.Name, pkg.ActiveInstalls, pkg.WpComposerInstallsTotal)
	a.Logger.Info("generated OG image", "package", pkg.Type+"/"+pkg.Name)
}

// Query helpers

type indexStats struct {
	PluginInstalls int64
	ThemeInstalls  int64
	RootsDownloads int64
}

func queryIndexStats(ctx context.Context, db *sql.DB) indexStats {
	var s indexStats
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(SUM(wp_composer_installs_total), 0) FROM packages WHERE type = 'plugin'").Scan(&s.PluginInstalls)
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(SUM(wp_composer_installs_total), 0) FROM packages WHERE type = 'theme'").Scan(&s.ThemeInstalls)
	return s
}

func queryPackages(ctx context.Context, db *sql.DB, f publicFilters, page, limit int) ([]packageRow, int, error) {
	where := "is_active = 1"
	args := []any{}

	if f.Search != "" {
		normalized := "%" + strings.NewReplacer("-", "", " ", "").Replace(f.Search) + "%"
		s := "%" + f.Search + "%"
		where += " AND (REPLACE(REPLACE(name, '-', ''), ' ', '') LIKE ? OR display_name LIKE ? OR description LIKE ?)"
		args = append(args, normalized, s, s)
	}
	if f.Type != "" {
		where += " AND type = ?"
		args = append(args, f.Type)
	}

	orderBy := "active_installs DESC"
	switch f.Sort {
	case "composer_installs":
		orderBy = "wp_composer_installs_total DESC"
	case "updated":
		orderBy = "last_committed DESC NULLS LAST"
	case "name":
		orderBy = "name ASC"
	}

	var total int
	countQ := "SELECT COUNT(*) FROM packages WHERE " + where
	if err := db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	q := fmt.Sprintf(`SELECT type, name, COALESCE(display_name,''), COALESCE(description,''),
		COALESCE(current_version,''), downloads, active_installs, wp_composer_installs_total
		FROM packages WHERE %s ORDER BY %s LIMIT ? OFFSET ?`, where, orderBy)

	rows, err := db.QueryContext(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var pkgs []packageRow
	for rows.Next() {
		var p packageRow
		if err := rows.Scan(&p.Type, &p.Name, &p.DisplayName, &p.Description, &p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpComposerInstallsTotal); err != nil {
			return nil, 0, fmt.Errorf("scanning package row: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, total, rows.Err()
}

type packageDetail struct {
	packageRow
	VersionsJSON       string
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
		downloads, active_installs, wp_composer_installs_total, versions_json, og_image_generated_at
		FROM packages WHERE type = ? AND name = ? AND is_active = 1`, pkgType, name,
	).Scan(&p.Type, &p.Name, &p.DisplayName, &p.Description, &p.Author, &p.Homepage,
		&p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpComposerInstallsTotal,
		&p.VersionsJSON, &p.OGImageGeneratedAt)
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
		return compareVersions(b.Version, a.Version)
	})
	return rows
}

// compareVersions compares two version strings numerically by segment.
// Returns -1, 0, or 1.
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := range maxLen {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if c := cmp.Compare(av, bv); c != 0 {
			return c
		}
	}
	return 0
}

func queryDashboardStats(ctx context.Context, db *sql.DB) map[string]any {
	stats := map[string]any{
		"Stats": struct {
			TotalPackages int64
			ActivePlugins int64
			ActiveThemes  int64
			TotalInstalls int64
			Installs30d   int64
			CurrentBuild  string
		}{},
	}

	var s struct {
		TotalPackages int64
		ActivePlugins int64
		ActiveThemes  int64
		TotalInstalls int64
		Installs30d   int64
		CurrentBuild  string
	}

	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM packages WHERE is_active = 1").Scan(&s.TotalPackages)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM packages WHERE is_active = 1 AND type = 'plugin'").Scan(&s.ActivePlugins)
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM packages WHERE is_active = 1 AND type = 'theme'").Scan(&s.ActiveThemes)
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(SUM(wp_composer_installs_total), 0) FROM packages").Scan(&s.TotalInstalls)
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(SUM(wp_composer_installs_30d), 0) FROM packages").Scan(&s.Installs30d)

	stats["Stats"] = s
	return stats
}

func queryAdminPackages(ctx context.Context, db *sql.DB, f adminFilters, page, limit int) ([]packageRow, int, error) {
	where := "1=1"
	args := []any{}

	if f.Search != "" {
		normalized := "%" + strings.NewReplacer("-", "", " ", "").Replace(f.Search) + "%"
		s := "%" + f.Search + "%"
		where += " AND (REPLACE(REPLACE(name, '-', ''), ' ', '') LIKE ? OR display_name LIKE ? OR description LIKE ?)"
		args = append(args, normalized, s, s)
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
		downloads, active_installs, wp_composer_installs_total, is_active, COALESCE(last_synced_at,'')
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
		_ = rows.Scan(&p.Type, &p.Name, &p.DisplayName, &p.CurrentVersion, &p.Downloads, &p.ActiveInstalls, &p.WpComposerInstallsTotal, &isActive, &p.LastSyncedAt)
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
}

func queryBuilds(ctx context.Context, db *sql.DB) ([]buildRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, started_at, packages_total, packages_changed,
		artifact_count, status, COALESCE(r2_synced_at, ''), COALESCE(error_message, '')
		FROM builds ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var builds []buildRow
	for rows.Next() {
		var b buildRow
		_ = rows.Scan(&b.ID, &b.StartedAt, &b.PackagesTotal, &b.PackagesChanged, &b.ArtifactCount, &b.Status, &b.R2SyncedAt, &b.ErrorMessage)
		builds = append(builds, b)
	}
	return builds, rows.Err()
}
