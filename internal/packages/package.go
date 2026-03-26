package packages

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/roots/wp-packages/internal/version"
)

type Package struct {
	ID                      int64
	Type                    string
	Name                    string
	DisplayName             *string
	Description             *string
	Author                  *string
	Homepage                *string
	SlugURL                 *string
	VersionsJSON            string
	Downloads               int64
	ActiveInstalls          int64
	CurrentVersion          *string
	WporgVersion            *string
	Rating                  *float64
	NumRatings              int
	IsActive                bool
	LastCommitted           *time.Time
	LastSyncedAt            *time.Time
	LastSyncRunID           *int64
	TrunkRevision           *int64
	WpPackagesInstallsTotal int
	WpPackagesInstalls30d   int
	LastInstalledAt         *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time

	// RawVersions holds the pre-normalization version map from the API.
	// Not persisted directly — normalized into VersionsJSON before storage.
	RawVersions map[string]string `json:"-"`
}

// NormalizeAndStoreVersions normalizes raw versions and serializes to VersionsJSON.
// It also sets CurrentVersion to the highest available version.
// Returns the number of valid versions.
func (p *Package) NormalizeAndStoreVersions() (int, error) {
	if p.RawVersions == nil {
		p.VersionsJSON = "{}"
		return 0, nil
	}

	normalized := version.NormalizeVersions(p.RawVersions)

	data, err := json.Marshal(normalized)
	if err != nil {
		return 0, fmt.Errorf("marshaling versions: %w", err)
	}
	p.VersionsJSON = string(data)

	if latest := version.Latest(normalized); latest != "" {
		p.CurrentVersion = &latest
	}

	return len(normalized), nil
}

func timeStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// UpsertPackage inserts or updates a package record by (type, name).
func UpsertPackage(ctx context.Context, db *sql.DB, pkg *Package) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.ExecContext(ctx, `
		INSERT INTO packages (
			type, name, display_name, description, author, homepage, slug_url,
			versions_json, downloads, active_installs,
			current_version, wporg_version, rating, num_ratings, is_active,
			last_committed, last_synced_at, last_sync_run_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, name) DO UPDATE SET
			display_name = excluded.display_name,
			description = excluded.description,
			author = excluded.author,
			homepage = excluded.homepage,
			slug_url = excluded.slug_url,
			versions_json = excluded.versions_json,
			downloads = excluded.downloads,
			active_installs = excluded.active_installs,
			current_version = excluded.current_version,
			wporg_version = excluded.wporg_version,
			rating = excluded.rating,
			num_ratings = excluded.num_ratings,
			is_active = excluded.is_active,
			last_committed = CASE
				WHEN excluded.last_committed > COALESCE(packages.last_committed, '')
				THEN excluded.last_committed
				ELSE packages.last_committed
			END,
			last_synced_at = COALESCE(excluded.last_synced_at, packages.last_synced_at),
			last_sync_run_id = COALESCE(excluded.last_sync_run_id, packages.last_sync_run_id),
			updated_at = excluded.updated_at`,
		pkg.Type, pkg.Name, pkg.DisplayName, pkg.Description, pkg.Author,
		pkg.Homepage, pkg.SlugURL, pkg.VersionsJSON,
		pkg.Downloads, pkg.ActiveInstalls, pkg.CurrentVersion, pkg.WporgVersion, pkg.Rating,
		pkg.NumRatings, boolToInt(pkg.IsActive),
		timeStr(pkg.LastCommitted), timeStr(pkg.LastSyncedAt), pkg.LastSyncRunID,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("upserting package %s/%s: %w", pkg.Type, pkg.Name, err)
	}
	return nil
}

// UpsertShellPackage creates a minimal package record (for SVN discovery) or
// updates last_committed if the new date is more recent.
func UpsertShellPackage(ctx context.Context, db *sql.DB, pkgType, name string, lastCommitted *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.ExecContext(ctx, `
		INSERT INTO packages (type, name, last_committed, is_active, versions_json, created_at, updated_at)
		VALUES (?, ?, ?, 1, '{}', ?, ?)
		ON CONFLICT(type, name) DO UPDATE SET
			last_committed = CASE
				WHEN excluded.last_committed > COALESCE(packages.last_committed, '')
				THEN excluded.last_committed
				ELSE packages.last_committed
			END,
			updated_at = excluded.updated_at`,
		pkgType, name, timeStr(lastCommitted), now, now,
	)
	if err != nil {
		return fmt.Errorf("upserting shell package %s/%s: %w", pkgType, name, err)
	}
	return nil
}

// ShellEntry holds minimal package data for batch SVN discovery upserts.
type ShellEntry struct {
	Type          string
	Name          string
	LastCommitted *time.Time
}

// BatchUpsertShellPackages inserts or updates shell package records in a single transaction.
func BatchUpsertShellPackages(ctx context.Context, db *sql.DB, entries []ShellEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO packages (type, name, last_committed, is_active, versions_json, created_at, updated_at)
		VALUES (?, ?, ?, 1, '{}', ?, ?)
		ON CONFLICT(type, name) DO UPDATE SET
			last_committed = CASE
				WHEN excluded.last_committed > COALESCE(packages.last_committed, '')
				THEN excluded.last_committed
				ELSE packages.last_committed
			END,
			updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.Type, e.Name, timeStr(e.LastCommitted), now, now); err != nil {
			return fmt.Errorf("upserting shell package %s/%s: %w", e.Type, e.Name, err)
		}
	}
	return tx.Commit()
}

// BatchUpsertPackages inserts or updates full package records in a single transaction.
func BatchUpsertPackages(ctx context.Context, db *sql.DB, pkgs []*Package) error {
	if len(pkgs) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO packages (
			type, name, display_name, description, author, homepage, slug_url,
			versions_json, downloads, active_installs,
			current_version, wporg_version, rating, num_ratings, is_active,
			last_committed, last_synced_at, last_sync_run_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(type, name) DO UPDATE SET
			display_name = excluded.display_name,
			description = excluded.description,
			author = excluded.author,
			homepage = excluded.homepage,
			slug_url = excluded.slug_url,
			versions_json = excluded.versions_json,
			downloads = excluded.downloads,
			active_installs = excluded.active_installs,
			current_version = excluded.current_version,
			wporg_version = excluded.wporg_version,
			rating = excluded.rating,
			num_ratings = excluded.num_ratings,
			is_active = excluded.is_active,
			last_committed = CASE
				WHEN excluded.last_committed > COALESCE(packages.last_committed, '')
				THEN excluded.last_committed
				ELSE packages.last_committed
			END,
			last_synced_at = COALESCE(excluded.last_synced_at, packages.last_synced_at),
			last_sync_run_id = COALESCE(excluded.last_sync_run_id, packages.last_sync_run_id),
			updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, pkg := range pkgs {
		if _, err := stmt.ExecContext(ctx,
			pkg.Type, pkg.Name, pkg.DisplayName, pkg.Description, pkg.Author,
			pkg.Homepage, pkg.SlugURL, pkg.VersionsJSON,
			pkg.Downloads, pkg.ActiveInstalls, pkg.CurrentVersion, pkg.WporgVersion, pkg.Rating,
			pkg.NumRatings, boolToInt(pkg.IsActive),
			timeStr(pkg.LastCommitted), timeStr(pkg.LastSyncedAt), pkg.LastSyncRunID,
			now, now,
		); err != nil {
			return fmt.Errorf("upserting package %s/%s: %w", pkg.Type, pkg.Name, err)
		}
	}
	return tx.Commit()
}

type UpdateQueryOpts struct {
	Type            string
	Name            string
	Names           []string // filter to these slugs only
	Force           bool
	IncludeInactive bool
	Limit           int
}

// GetPackagesNeedingUpdate returns packages that should be updated.
func GetPackagesNeedingUpdate(ctx context.Context, db *sql.DB, opts UpdateQueryOpts) ([]*Package, error) {
	query := `SELECT id, type, name, last_committed, last_synced_at, is_active FROM packages WHERE 1=1`
	var args []any

	if opts.Name != "" {
		query += ` AND name = ?`
		args = append(args, opts.Name)
	}

	if len(opts.Names) > 0 {
		placeholders := make([]string, len(opts.Names))
		for i, n := range opts.Names {
			placeholders[i] = "?"
			args = append(args, n)
		}
		query += ` AND name IN (` + strings.Join(placeholders, ",") + `)`
	}

	if opts.Type != "" && opts.Type != "all" {
		query += ` AND type = ?`
		args = append(args, opts.Type)
	}

	if !opts.Force && opts.Name == "" {
		if opts.IncludeInactive {
			query += ` AND (last_synced_at IS NULL OR last_committed > last_synced_at OR (is_active = 0 AND (last_synced_at IS NULL OR last_synced_at < datetime('now', '-30 days'))))`
		} else {
			query += ` AND is_active = 1 AND (last_synced_at IS NULL OR last_committed > last_synced_at)`
		}
	} else if !opts.IncludeInactive && opts.Name == "" {
		query += ` AND is_active = 1`
	}

	query += ` ORDER BY last_synced_at ASC NULLS FIRST`

	if opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, opts.Limit)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying packages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pkgs []*Package
	for rows.Next() {
		var p Package
		var isActive int
		var lastCommitted, lastSyncedAt *string
		if err := rows.Scan(&p.ID, &p.Type, &p.Name, &lastCommitted, &lastSyncedAt, &isActive); err != nil {
			return nil, fmt.Errorf("scanning package row: %w", err)
		}
		p.IsActive = isActive == 1
		if lastCommitted != nil {
			if t, err := time.Parse(time.RFC3339, *lastCommitted); err == nil {
				p.LastCommitted = &t
			}
		}
		if lastSyncedAt != nil {
			if t, err := time.Parse(time.RFC3339, *lastSyncedAt); err == nil {
				p.LastSyncedAt = &t
			}
		}
		pkgs = append(pkgs, &p)
	}
	return pkgs, rows.Err()
}

// DeactivatePackage sets is_active = 0 for a package.
func DeactivatePackage(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE packages SET is_active = 0, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("deactivating package %d: %w", id, err)
	}
	return nil
}

// RefreshSiteStats recomputes the package_stats row from the packages table.
func RefreshSiteStats(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT OR REPLACE INTO package_stats (id, active_plugins, active_themes, plugin_installs, theme_installs, installs_30d, updated_at)
		SELECT 1,
			COALESCE(SUM(CASE WHEN type = 'plugin' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'theme' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'plugin' THEN wp_packages_installs_total ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN type = 'theme' THEN wp_packages_installs_total ELSE 0 END), 0),
			COALESCE(SUM(wp_packages_installs_30d), 0),
			datetime('now')
		FROM packages
		WHERE is_active = 1`)
	if err != nil {
		return fmt.Errorf("refreshing package stats: %w", err)
	}
	return nil
}

// MarkPackagesChanged sets last_committed = now and trunk_revision for the given
// slugs of a specific type, so they'll be picked up by GetPackagesNeedingUpdate.
// slugRevisions maps each slug to its highest SVN revision from the changelog.
func MarkPackagesChanged(ctx context.Context, db *sql.DB, pkgType string, slugRevisions map[string]int64) (int64, error) {
	if len(slugRevisions) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE packages
		SET last_committed = ?, updated_at = ?, trunk_revision = ?
		WHERE type = ? AND name = ? AND is_active = 1`)
	if err != nil {
		return 0, fmt.Errorf("preparing statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	var affected int64
	for slug, rev := range slugRevisions {
		res, err := stmt.ExecContext(ctx, now, now, rev, pkgType, slug)
		if err != nil {
			return affected, fmt.Errorf("marking package %s/%s changed: %w", pkgType, slug, err)
		}
		n, _ := res.RowsAffected()
		affected += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing: %w", err)
	}
	return affected, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
