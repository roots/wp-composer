package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// BuildOpts configures a repository build.
type BuildOpts struct {
	OutputDir   string // base output dir (e.g. storage/repository/builds)
	AppURL      string // absolute app URL for notify-batch
	Force       bool
	PackageName string // optional: build single package
	Logger      *slog.Logger
}

// BuildResult holds build metadata for manifest.json and the builds table.
type BuildResult struct {
	BuildID         string
	StartedAt       time.Time
	FinishedAt      time.Time
	DurationSeconds int
	PackagesTotal   int
	PackagesChanged int
	PackagesSkipped int
	ProviderGroups  int
	ArtifactCount   int
	RootHash        string
	SyncRunID       *int64
	BuildDir        string
}

// Build generates all Composer repository artifacts.
func Build(ctx context.Context, db *sql.DB, opts BuildOpts) (*BuildResult, error) {
	started := time.Now().UTC()
	buildID := started.Format("20060102-150405")
	buildDir := filepath.Join(opts.OutputDir, buildID)

	// Guard against build ID collision
	if _, err := os.Stat(buildDir); err == nil {
		return nil, fmt.Errorf("build directory already exists: %s (another build started in the same second?)", buildID)
	}

	if err := os.MkdirAll(filepath.Join(buildDir, "p"), 0755); err != nil {
		return nil, fmt.Errorf("creating build dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(buildDir, "p2"), 0755); err != nil {
		return nil, fmt.Errorf("creating p2 dir: %w", err)
	}

	opts.Logger.Info("starting build", "build_id", buildID)

	// Snapshot sync run ID for consistency (skip with --force)
	var snapshotID *int64
	if !opts.Force {
		var sid int64
		err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(last_sync_run_id), 0) FROM packages`).Scan(&sid)
		if err != nil {
			return nil, fmt.Errorf("getting snapshot id: %w", err)
		}
		if sid > 0 {
			snapshotID = &sid
		}
	}

	// Query active packages
	query := `SELECT id, type, name, display_name, description, author, homepage,
		provider_group, versions_json, current_version, last_committed
		FROM packages WHERE is_active = 1`
	args := []any{}

	if snapshotID != nil {
		query += ` AND (last_sync_run_id IS NULL OR last_sync_run_id <= ?)`
		args = append(args, *snapshotID)
	}
	if opts.PackageName != "" {
		query += ` AND (type || '/' || name) = ?`
		args = append(args, opts.PackageName)
	}
	query += ` ORDER BY type, name`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying packages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// packageHashes: composerName -> hash (for provider files)
	packageHashes := make(map[string]string)
	// providerPackages: providerGroup -> []composerName
	providerPackages := make(map[string][]string)
	var totalPkgs, changedPkgs, artifactCount int

	for rows.Next() {
		var (
			id                                                        int64
			pkgType, name                                             string
			displayName, description, author, homepage, providerGroup *string
			versionsJSON                                              string
			currentVer                                                *string
			lastCommitted                                             *string
		)
		if err := rows.Scan(&id, &pkgType, &name, &displayName, &description, &author,
			&homepage, &providerGroup, &versionsJSON, &currentVer, &lastCommitted); err != nil {
			return nil, fmt.Errorf("scanning package: %w", err)
		}

		// Parse versions
		var versions map[string]string
		if err := json.Unmarshal([]byte(versionsJSON), &versions); err != nil {
			opts.Logger.Warn("skipping package with invalid versions_json", "name", name, "error", err)
			continue
		}
		if len(versions) == 0 {
			continue
		}

		totalPkgs++
		composerName := ComposerName(pkgType, name)
		meta := PackageMeta{}
		if description != nil {
			meta.Description = *description
		}
		if homepage != nil {
			meta.Homepage = *homepage
		}
		if author != nil {
			meta.Author = *author
		}
		if lastCommitted != nil {
			meta.LastUpdated = *lastCommitted
		}

		// Build per-version entries
		composerVersions := make(map[string]any, len(versions))
		for ver, dlURL := range versions {
			composerVersions[ver] = ComposerVersion(pkgType, name, ver, dlURL, meta)
		}

		// Write p/ file (content-addressed)
		pkgPayload := map[string]any{
			"packages": map[string]any{
				composerName: composerVersions,
			},
		}
		hash, data, err := HashJSON(pkgPayload)
		if err != nil {
			return nil, fmt.Errorf("hashing %s: %w", composerName, err)
		}

		pkgDir := filepath.Join(buildDir, "p", ComposerName(pkgType, name))
		if err := os.MkdirAll(filepath.Dir(pkgDir), 0755); err != nil {
			return nil, fmt.Errorf("creating p dir for %s: %w", composerName, err)
		}
		pkgFile := fmt.Sprintf("%s$%s.json", pkgDir, hash)
		if err := os.WriteFile(pkgFile, data, 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", pkgFile, err)
		}
		packageHashes[composerName] = hash
		artifactCount++
		changedPkgs++

		// Write p2/ file
		p2Dir := filepath.Join(buildDir, "p2", ComposerName(pkgType, name))
		if err := os.MkdirAll(filepath.Dir(p2Dir), 0755); err != nil {
			return nil, fmt.Errorf("creating p2 dir for %s: %w", composerName, err)
		}
		p2Payload := map[string]any{
			"packages": map[string]any{
				composerName: composerVersions,
			},
		}
		p2Data, err := DeterministicJSON(p2Payload)
		if err != nil {
			return nil, fmt.Errorf("encoding p2 %s: %w", composerName, err)
		}
		if err := os.WriteFile(p2Dir+".json", p2Data, 0644); err != nil {
			return nil, fmt.Errorf("writing p2 %s: %w", composerName, err)
		}
		artifactCount++

		// Track provider group
		group := "unknown"
		if providerGroup != nil {
			group = *providerGroup
		}
		providerPackages[group] = append(providerPackages[group], composerName)

		if totalPkgs%500 == 0 {
			opts.Logger.Info("build progress", "packages", totalPkgs)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating packages: %w", err)
	}

	// Build provider group files
	providerIncludes := make(map[string]map[string]string)
	for group, names := range providerPackages {
		providers := make(map[string]map[string]string, len(names))
		for _, name := range names {
			providers[name] = map[string]string{"sha256": packageHashes[name]}
		}
		payload := map[string]any{"providers": providers}
		hash, data, err := HashJSON(payload)
		if err != nil {
			return nil, fmt.Errorf("hashing provider group %s: %w", group, err)
		}

		filename := fmt.Sprintf("providers-%s$%s.json", group, hash)
		if err := os.WriteFile(filepath.Join(buildDir, "p", filename), data, 0644); err != nil {
			return nil, fmt.Errorf("writing provider %s: %w", filename, err)
		}
		providerIncludes[fmt.Sprintf("p/%s", filename)] = map[string]string{"sha256": hash}
		artifactCount++
	}

	// Build packages.json
	notifyBatch := "/downloads"
	if opts.AppURL != "" {
		notifyBatch = opts.AppURL + "/downloads"
	}

	packagesJSON := map[string]any{
		"packages":                   map[string]any{},
		"notify-batch":               notifyBatch,
		"metadata-url":               "/p2/%package%.json",
		"providers-url":              "/p/%package%$%hash%.json",
		"provider-includes":          providerIncludes,
		"available-package-patterns": []string{"wp-plugin/*", "wp-theme/*"},
	}

	rootHash, rootData, err := HashJSON(packagesJSON)
	if err != nil {
		return nil, fmt.Errorf("hashing packages.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "packages.json"), rootData, 0644); err != nil {
		return nil, fmt.Errorf("writing packages.json: %w", err)
	}
	artifactCount++

	// Write manifest.json
	finished := time.Now().UTC()
	manifest := map[string]any{
		"build_id":         buildID,
		"started_at":       started.Format(time.RFC3339),
		"finished_at":      finished.Format(time.RFC3339),
		"duration_seconds": int(finished.Sub(started).Seconds()),
		"packages_total":   totalPkgs,
		"packages_changed": changedPkgs,
		"packages_skipped": totalPkgs - changedPkgs,
		"provider_groups":  len(providerPackages),
		"artifact_count":   artifactCount,
		"root_hash":        rootHash,
	}
	if snapshotID != nil {
		manifest["db_snapshot_id"] = *snapshotID
	}

	manifestData, _ := DeterministicJSON(manifest)
	if err := os.WriteFile(filepath.Join(buildDir, "manifest.json"), manifestData, 0644); err != nil {
		return nil, fmt.Errorf("writing manifest.json: %w", err)
	}
	artifactCount++

	// Validate integrity
	errors := ValidateIntegrity(buildDir)
	if len(errors) > 0 {
		for _, e := range errors {
			opts.Logger.Error("integrity error", "error", e)
		}
		return nil, fmt.Errorf("integrity validation failed with %d errors", len(errors))
	}

	result := &BuildResult{
		BuildID:         buildID,
		StartedAt:       started,
		FinishedAt:      finished,
		DurationSeconds: int(finished.Sub(started).Seconds()),
		PackagesTotal:   totalPkgs,
		PackagesChanged: changedPkgs,
		PackagesSkipped: totalPkgs - changedPkgs,
		ProviderGroups:  len(providerPackages),
		ArtifactCount:   artifactCount,
		RootHash:        rootHash,
		SyncRunID:       snapshotID,
		BuildDir:        buildDir,
	}

	opts.Logger.Info("build complete",
		"build_id", buildID,
		"packages", totalPkgs,
		"artifacts", artifactCount,
		"duration", finished.Sub(started).String(),
	)

	return result, nil
}

// ValidateIntegrity checks that all hash references in packages.json resolve to actual files
// and that file content matches the declared SHA-256 hash.
func ValidateIntegrity(buildDir string) []string {
	var errors []string

	packagesPath := filepath.Join(buildDir, "packages.json")
	data, err := os.ReadFile(packagesPath)
	if err != nil {
		return []string{fmt.Sprintf("packages.json missing: %v", err)}
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return []string{fmt.Sprintf("packages.json invalid: %v", err)}
	}

	includes, ok := root["provider-includes"].(map[string]any)
	if !ok {
		return []string{"provider-includes missing or invalid"}
	}

	for providerPath, includeInfo := range includes {
		// Verify provider file hash
		info, _ := includeInfo.(map[string]any)
		declaredHash, _ := info["sha256"].(string)

		fullPath := filepath.Join(buildDir, providerPath)
		providerData, err := os.ReadFile(fullPath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("provider file missing: %s", providerPath))
			continue
		}

		if declaredHash != "" {
			actualHash := fmt.Sprintf("%x", sha256.Sum256(providerData))
			if actualHash != declaredHash {
				errors = append(errors, fmt.Sprintf("provider hash mismatch: %s (declared=%s actual=%s)", providerPath, declaredHash, actualHash))
			}
		}

		var provider map[string]any
		if err := json.Unmarshal(providerData, &provider); err != nil {
			errors = append(errors, fmt.Sprintf("provider file invalid: %s", providerPath))
			continue
		}

		providers, ok := provider["providers"].(map[string]any)
		if !ok {
			continue
		}

		for pkgName, hashInfo := range providers {
			pkgInfo, ok := hashInfo.(map[string]any)
			if !ok {
				continue
			}
			hash, ok := pkgInfo["sha256"].(string)
			if !ok {
				continue
			}
			pkgPath := filepath.Join(buildDir, "p", fmt.Sprintf("%s$%s.json", pkgName, hash))
			pkgData, err := os.ReadFile(pkgPath)
			if err != nil {
				errors = append(errors, fmt.Sprintf("package file missing: p/%s$%s.json", pkgName, hash))
				continue
			}

			actualHash := fmt.Sprintf("%x", sha256.Sum256(pkgData))
			if actualHash != hash {
				errors = append(errors, fmt.Sprintf("package hash mismatch: %s (declared=%s actual=%s)", pkgName, hash, actualHash))
			}
		}
	}

	return errors
}
