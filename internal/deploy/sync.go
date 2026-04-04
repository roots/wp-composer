package deploy

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/roots/wp-packages/internal/composer"
	"github.com/roots/wp-packages/internal/config"
)

// SyncResult holds statistics from a DB-driven R2 sync.
type SyncResult struct {
	Uploaded int64
	Deleted  int64
	Skipped  int64
	Duration time.Duration
}

// Sync uploads changed packages from the database to R2.
//
// It queries for packages where content_hash != deployed_hash, serializes
// them into Composer p2 JSON files, uploads to R2 in parallel, deletes
// p2 files for deactivated packages, conditionally uploads packages.json,
// and stamps deployed_hash on success.
func Sync(ctx context.Context, db *sql.DB, cfg config.R2Config, appURL string, logger *slog.Logger) (*SyncResult, error) {
	started := time.Now()
	client := newS3Client(cfg)

	var uploaded, deleted, skipped atomic.Int64

	// Step 1: Upload changed p2/ files
	rows, err := db.QueryContext(ctx, `
		SELECT id, type, name, versions_json, content_hash,
			description, homepage, author, last_committed, trunk_revision
		FROM packages
		WHERE is_active = 1
			AND content_hash IS NOT NULL
			AND (deployed_hash IS NULL OR content_hash != deployed_hash)`)
	if err != nil {
		return nil, fmt.Errorf("querying dirty packages: %w", err)
	}

	type dirtyPkg struct {
		id            int64
		pkgType, name string
		versionsJSON  string
		contentHash   string
		meta          composer.PackageMeta
	}

	var dirty []dirtyPkg
	for rows.Next() {
		var p dirtyPkg
		var description, homepage, author, lastCommitted *string
		var trunkRevision *int64

		if err := rows.Scan(&p.id, &p.pkgType, &p.name, &p.versionsJSON, &p.contentHash,
			&description, &homepage, &author, &lastCommitted, &trunkRevision); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scanning dirty package: %w", err)
		}

		if description != nil {
			p.meta.Description = *description
		}
		if homepage != nil {
			p.meta.Homepage = *homepage
		}
		if author != nil {
			p.meta.Author = *author
		}
		if lastCommitted != nil {
			p.meta.LastUpdated = *lastCommitted
		}
		p.meta.TrunkRevision = trunkRevision
		dirty = append(dirty, p)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing dirty packages query: %w", err)
	}

	logger.Info("sync: dirty packages", "count", len(dirty))

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(50)

	for _, p := range dirty {
		p := p
		g.Go(func() error {
			composerName := composer.ComposerName(p.pkgType, p.name)

			// Tagged versions file (always)
			taggedData, err := composer.SerializePackage(p.pkgType, p.name, p.versionsJSON, p.meta)
			if err != nil {
				return fmt.Errorf("serializing %s: %w", composerName, err)
			}
			if taggedData != nil {
				key := "p2/" + composerName + ".json"
				if err := putObjectWithRetry(gCtx, client, cfg.Bucket, key, taggedData, logger); err != nil {
					return fmt.Errorf("uploading %s: %w", key, err)
				}
				uploaded.Add(1)
			}

			// Dev versions file (plugins only)
			devData, err := composer.SerializePackage(p.pkgType, p.name+"~dev", p.versionsJSON, p.meta)
			if err != nil {
				return fmt.Errorf("serializing %s~dev: %w", composerName, err)
			}
			if devData != nil {
				key := "p2/" + composerName + "~dev.json"
				if err := putObjectWithRetry(gCtx, client, cfg.Bucket, key, devData, logger); err != nil {
					return fmt.Errorf("uploading %s: %w", key, err)
				}
				uploaded.Add(1)
			}

			n := uploaded.Load() + skipped.Load()
			if n%500 == 0 && n > 0 {
				logger.Info("sync: upload progress", "uploaded", uploaded.Load(), "total_dirty", len(dirty))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Step 2: Delete p2/ files for deactivated packages
	deactivatedRows, err := db.QueryContext(ctx, `
		SELECT type, name FROM packages
		WHERE is_active = 0 AND deployed_hash IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("querying deactivated packages: %w", err)
	}

	type deactivatedPkg struct {
		pkgType, name string
	}
	var deactivated []deactivatedPkg
	for deactivatedRows.Next() {
		var p deactivatedPkg
		if err := deactivatedRows.Scan(&p.pkgType, &p.name); err != nil {
			_ = deactivatedRows.Close()
			return nil, fmt.Errorf("scanning deactivated package: %w", err)
		}
		deactivated = append(deactivated, p)
	}
	_ = deactivatedRows.Close()

	for _, p := range deactivated {
		composerName := composer.ComposerName(p.pkgType, p.name)
		for _, suffix := range []string{".json", "~dev.json"} {
			key := "p2/" + composerName + suffix
			if err := deleteObjectWithRetry(ctx, client, cfg.Bucket, key, logger); err != nil {
				logger.Warn("sync: failed to delete deactivated package file", "key", key, "error", err)
				continue
			}
		}
		deleted.Add(1)
		logger.Info("sync: deleted deactivated package", "package", composerName)
	}

	// Step 3: Conditional packages.json upload
	packagesData, err := composer.PackagesJSON(appURL)
	if err != nil {
		return nil, fmt.Errorf("generating packages.json: %w", err)
	}

	currentETag, _ := headObject(ctx, client, cfg.Bucket, r2IndexFile)
	newETag := fmt.Sprintf(`"%x"`, md5.Sum(packagesData))
	if currentETag != newETag {
		if err := putObjectWithRetry(ctx, client, cfg.Bucket, r2IndexFile, packagesData, logger); err != nil {
			return nil, fmt.Errorf("uploading packages.json: %w", err)
		}
		logger.Info("sync: uploaded packages.json")
	} else {
		logger.Info("sync: packages.json unchanged, skipped")
	}

	// Step 4: Stamp deployed_hash
	if len(dirty) > 0 {
		_, err = db.ExecContext(ctx, `
			UPDATE packages SET deployed_hash = content_hash
			WHERE is_active = 1 AND content_hash IS NOT NULL
				AND (deployed_hash IS NULL OR content_hash != deployed_hash)`)
		if err != nil {
			return nil, fmt.Errorf("stamping deployed_hash: %w", err)
		}
	}

	if len(deactivated) > 0 {
		_, err = db.ExecContext(ctx, `
			UPDATE packages SET deployed_hash = NULL
			WHERE is_active = 0 AND deployed_hash IS NOT NULL`)
		if err != nil {
			return nil, fmt.Errorf("clearing deployed_hash for deactivated: %w", err)
		}
	}

	result := &SyncResult{
		Uploaded: uploaded.Load(),
		Deleted:  deleted.Load(),
		Skipped:  skipped.Load(),
		Duration: time.Since(started),
	}

	logger.Info("sync: complete",
		"uploaded", result.Uploaded,
		"deleted", result.Deleted,
		"duration", result.Duration.String(),
	)
	return result, nil
}
