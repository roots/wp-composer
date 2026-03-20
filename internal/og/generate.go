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
	WpPackagesInstallsTotal int64
	OGImageGeneratedAt      *string
	OGImageInstalls         int64
	OGImageWpInstalls       int64
}

// FormatInstalls returns a human-readable install count with rounding
// to reduce unnecessary OG image regeneration when counts change slightly.
func FormatInstalls(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	if n >= 100 {
		rounded := (n / 100) * 100
		return fmt.Sprintf("%d+", rounded)
	}
	if n >= 10 {
		rounded := (n / 10) * 10
		return fmt.Sprintf("%d+", rounded)
	}
	return fmt.Sprintf("%d", n)
}

// getPackagesNeedingOG returns packages that need OG image generation:
// - Never generated (og_image_generated_at IS NULL)
// - Install counts changed since last generation
func getPackagesNeedingOG(ctx context.Context, db *sql.DB, limit int) ([]PackageOGRow, error) {
	q := `SELECT id, type, name, COALESCE(display_name, ''), COALESCE(description, ''),
		COALESCE(current_version, ''), active_installs, wp_packages_installs_total,
		og_image_generated_at, og_image_installs, og_image_wp_installs
		FROM packages
		WHERE is_active = 1
		AND (
			og_image_generated_at IS NULL
			OR active_installs != og_image_installs
			OR wp_packages_installs_total != og_image_wp_installs
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
			&p.CurrentVersion, &p.ActiveInstalls, &p.WpPackagesInstallsTotal,
			&p.OGImageGeneratedAt, &p.OGImageInstalls, &p.OGImageWpInstalls); err != nil {
			return nil, fmt.Errorf("scanning OG row: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, rows.Err()
}

// MarkOGGenerated updates the OG tracking columns after successful generation.
func markOGGenerated(ctx context.Context, db *sql.DB, id, activeInstalls, wpInstalls int64) error {
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

// GenerateResult holds the outcome of a generation run.
type GenerateResult struct {
	Generated int
	Skipped   int
	Errors    int
}

// ImageRenderer generates a PNG for the given package data.
type ImageRenderer func(PackageData) ([]byte, error)

// GenerateNew generates OG images for packages that have never had one.
func GenerateNew(ctx context.Context, db *sql.DB, uploader *Uploader, render ImageRenderer, logger *slog.Logger) (GenerateResult, error) {
	if render == nil {
		render = GeneratePackageImage
	}

	q := `SELECT id, type, name, COALESCE(display_name, ''), COALESCE(description, ''),
		COALESCE(current_version, ''), active_installs, wp_packages_installs_total
		FROM packages
		WHERE is_active = 1 AND og_image_generated_at IS NULL
		ORDER BY active_installs DESC`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("querying new packages for OG: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type newPkg struct {
		id, installs, wpInstalls                         int64
		pkgType, name, displayName, description, version string
	}

	var pkgs []newPkg
	for rows.Next() {
		var p newPkg
		if err := rows.Scan(&p.id, &p.pkgType, &p.name, &p.displayName, &p.description, &p.version, &p.installs, &p.wpInstalls); err != nil {
			return GenerateResult{}, fmt.Errorf("scanning OG row: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	if err := rows.Err(); err != nil {
		return GenerateResult{}, err
	}

	var result GenerateResult
	for _, pkg := range pkgs {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		data := PackageData{
			DisplayName:        pkg.displayName,
			Name:               pkg.name,
			Type:               pkg.pkgType,
			CurrentVersion:     pkg.version,
			Description:        pkg.description,
			ActiveInstalls:     FormatInstalls(pkg.installs),
			WpPackagesInstalls: FormatInstalls(pkg.wpInstalls),
		}

		if err := generateAndUpload(ctx, render, uploader, db, pkg.id, pkg.pkgType, pkg.name, data, pkg.installs, pkg.wpInstalls, logger); err != nil {
			result.Errors++
			continue
		}

		result.Generated++
		if result.Generated%100 == 0 {
			logger.Info("OG generation progress", "generated", result.Generated)
		}
	}

	return result, nil
}

func generateAndUpload(ctx context.Context, render ImageRenderer, uploader *Uploader, db *sql.DB, id int64, pkgType, name string, data PackageData, installs, wpInstalls int64, logger *slog.Logger) error {
	pngBytes, err := render(data)
	if err != nil {
		logger.Error("generating OG image", "package", name, "error", err)
		return err
	}

	key := fmt.Sprintf("social/%s/%s.png", pkgType, name)
	if err := uploader.Upload(ctx, key, pngBytes); err != nil {
		logger.Error("uploading OG image", "package", name, "error", err)
		return err
	}

	if err := markOGGenerated(ctx, db, id, installs, wpInstalls); err != nil {
		logger.Error("marking OG generated", "package", name, "error", err)
		return err
	}

	return nil
}

// GenerateAll generates OG images for all packages that need them.
func GenerateAll(ctx context.Context, db *sql.DB, uploader *Uploader, limit int, logger *slog.Logger) (GenerateResult, error) {
	pkgs, err := getPackagesNeedingOG(ctx, db, limit)
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

		// Skip if the formatted display values haven't changed
		if pkg.OGImageGeneratedAt != nil &&
			FormatInstalls(pkg.ActiveInstalls) == FormatInstalls(pkg.OGImageInstalls) &&
			FormatInstalls(pkg.WpPackagesInstallsTotal) == FormatInstalls(pkg.OGImageWpInstalls) {
			result.Skipped++
			continue
		}

		data := PackageData{
			DisplayName:        pkg.DisplayName,
			Name:               pkg.Name,
			Type:               pkg.Type,
			CurrentVersion:     pkg.CurrentVersion,
			Description:        pkg.Description,
			ActiveInstalls:     FormatInstalls(pkg.ActiveInstalls),
			WpPackagesInstalls: FormatInstalls(pkg.WpPackagesInstallsTotal),
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

		if err := markOGGenerated(ctx, db, pkg.ID, pkg.ActiveInstalls, pkg.WpPackagesInstallsTotal); err != nil {
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
