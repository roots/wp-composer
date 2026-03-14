package og

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// PackageOGRow holds the data needed for OG image generation decisions.
type PackageOGRow struct {
	ID                      int64
	Type                    string
	Name                    string
	DisplayName             string
	Description             string
	CurrentVersion          string
	ActiveInstalls          int64
	WpComposerInstallsTotal int64
	OGImageGeneratedAt      *string
	OGImageInstalls         int64
	OGImageWpInstalls       int64
}

// FormatInstalls returns a human-readable install count.
func FormatInstalls(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// GetPackagesNeedingOG returns packages that need OG image generation:
// - Never generated (og_image_generated_at IS NULL)
// - Install counts changed since last generation
// - Package metadata changed (current_version, display_name, description)
func GetPackagesNeedingOG(ctx context.Context, db *sql.DB, limit int) ([]PackageOGRow, error) {
	q := `SELECT id, type, name, COALESCE(display_name, ''), COALESCE(description, ''),
		COALESCE(current_version, ''), active_installs, wp_composer_installs_total,
		og_image_generated_at, og_image_installs, og_image_wp_installs
		FROM packages
		WHERE is_active = 1
		AND (
			og_image_generated_at IS NULL
			OR active_installs != og_image_installs
			OR wp_composer_installs_total != og_image_wp_installs
		)
		ORDER BY active_installs DESC
		LIMIT ?`

	rows, err := db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("querying packages for OG: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pkgs []PackageOGRow
	for rows.Next() {
		var p PackageOGRow
		if err := rows.Scan(&p.ID, &p.Type, &p.Name, &p.DisplayName, &p.Description,
			&p.CurrentVersion, &p.ActiveInstalls, &p.WpComposerInstallsTotal,
			&p.OGImageGeneratedAt, &p.OGImageInstalls, &p.OGImageWpInstalls); err != nil {
			return nil, fmt.Errorf("scanning OG row: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, rows.Err()
}

// MarkOGGenerated updates the OG tracking columns after successful generation.
func MarkOGGenerated(ctx context.Context, db *sql.DB, id, activeInstalls, wpInstalls int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE packages SET
		og_image_generated_at = ?,
		og_image_installs = ?,
		og_image_wp_installs = ?
		WHERE id = ?`, now, activeInstalls, wpInstalls, id)
	if err != nil {
		return fmt.Errorf("marking OG generated for package %d: %w", id, err)
	}
	return nil
}

// MarkOGGeneratedBySlug updates OG tracking columns by type+name.
func MarkOGGeneratedBySlug(ctx context.Context, db *sql.DB, pkgType, name string, activeInstalls, wpInstalls int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE packages SET
		og_image_generated_at = ?,
		og_image_installs = ?,
		og_image_wp_installs = ?
		WHERE type = ? AND name = ?`, now, activeInstalls, wpInstalls, pkgType, name)
	if err != nil {
		return fmt.Errorf("marking OG generated for %s/%s: %w", pkgType, name, err)
	}
	return nil
}

// GenerateResult holds the outcome of a generation run.
type GenerateResult struct {
	Generated int
	Skipped   int
	Errors    int
}

// GenerateAll generates OG images for all packages that need them.
func GenerateAll(ctx context.Context, db *sql.DB, uploader *Uploader, limit int, logger *slog.Logger) (GenerateResult, error) {
	pkgs, err := GetPackagesNeedingOG(ctx, db, limit)
	if err != nil {
		return GenerateResult{}, err
	}

	if len(pkgs) == 0 {
		logger.Info("no packages need OG image generation")
		return GenerateResult{}, nil
	}

	logger.Info("generating OG images", "packages", len(pkgs))

	var result GenerateResult
	for _, pkg := range pkgs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		data := PackageData{
			DisplayName:        pkg.DisplayName,
			Name:               pkg.Name,
			Type:               pkg.Type,
			CurrentVersion:     pkg.CurrentVersion,
			Description:        pkg.Description,
			ActiveInstalls:     FormatInstalls(pkg.ActiveInstalls),
			WpComposerInstalls: FormatInstalls(pkg.WpComposerInstallsTotal),
		}

		pngBytes, err := GeneratePackageImage(data)
		if err != nil {
			logger.Error("generating OG image", "package", pkg.Name, "error", err)
			result.Errors++
			continue
		}

		key := fmt.Sprintf("social/%s/%s.png", pkg.Type, pkg.Name)
		if err := uploader.Upload(ctx, key, pngBytes); err != nil {
			logger.Error("uploading OG image", "package", pkg.Name, "error", err)
			result.Errors++
			continue
		}

		if err := MarkOGGenerated(ctx, db, pkg.ID, pkg.ActiveInstalls, pkg.WpComposerInstallsTotal); err != nil {
			logger.Error("marking OG generated", "package", pkg.Name, "error", err)
			result.Errors++
			continue
		}

		result.Generated++
		if result.Generated%100 == 0 {
			logger.Info("OG generation progress", "generated", result.Generated, "total", len(pkgs))
		}
	}

	return result, nil
}
