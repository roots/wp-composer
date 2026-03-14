package cmd

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	wpcomposergo "github.com/roots/wp-composer"
	"github.com/roots/wp-composer/internal/auth"
	"github.com/roots/wp-composer/internal/db"
	"github.com/roots/wp-composer/internal/deploy"
	apphttp "github.com/roots/wp-composer/internal/http"
	"github.com/roots/wp-composer/internal/packages"
	"github.com/roots/wp-composer/internal/repository"
	"github.com/roots/wp-composer/internal/wporg"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Bootstrap database, seed packages, build artifacts, and start dev server",
	Long: `One-command local development setup:
  1. Run migrations
  2. Create admin user (admin@localhost / admin)
  3. Discover packages from seed config
  4. Fetch package metadata
  5. Build Composer repository artifacts
  6. Deploy (promote build)
  7. Start HTTP server`,
	RunE: runDev,
}

func runDev(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	addr, _ := cmd.Flags().GetString("addr")
	if addr != "" {
		application.Config.Server.Addr = addr
	}

	// 1. Migrations
	application.Logger.Info("dev: running migrations")
	if err := db.Migrate(application.DB, wpcomposergo.Migrations); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	// 2. Create admin user (idempotent)
	application.Logger.Info("dev: ensuring admin user")
	_, err := auth.GetUserByEmail(ctx, application.DB, "admin@localhost")
	if err != nil {
		hash, _ := auth.HashPassword("admin")
		_, err = auth.CreateUser(ctx, application.DB, "admin@localhost", "Admin", hash, true)
		if err != nil {
			return fmt.Errorf("creating admin: %w", err)
		}
		application.Logger.Info("dev: admin user created", "email", "admin@localhost", "password", "admin")
	} else {
		application.Logger.Info("dev: admin user already exists")
	}

	// 3. Discover from config
	application.Logger.Info("dev: discovering packages")
	concurrency := application.Config.Discovery.Concurrency
	if err := discoverFromConfig(ctx, "all", 0, concurrency); err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	// 4. Update all packages
	application.Logger.Info("dev: fetching package metadata")
	syncRun, err := packages.AllocateSyncRunID(ctx, application.DB)
	if err != nil {
		return fmt.Errorf("allocating sync run: %w", err)
	}

	pkgs, err := packages.GetPackagesNeedingUpdate(ctx, application.DB, packages.UpdateQueryOpts{
		Type:  "all",
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("querying packages: %w", err)
	}

	client := wporg.NewClient(application.Config.Discovery, application.Logger)
	var succeeded, failed atomic.Int64
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, p := range pkgs {
		p := p
		g.Go(func() error {
			var data map[string]any
			var fetchErr error
			if p.Type == "plugin" {
				data, fetchErr = client.FetchPlugin(gCtx, p.Name)
			} else {
				data, fetchErr = client.FetchTheme(gCtx, p.Name)
			}
			if fetchErr != nil {
				failed.Add(1)
				return nil
			}

			pkg := packages.PackageFromAPIData(data, p.Type)
			_, _ = pkg.NormalizeAndStoreVersions()

			now := time.Now().UTC()
			pkg.LastSyncedAt = &now
			pkg.LastSyncRunID = &syncRun.RunID

			if err := packages.UpsertPackage(gCtx, application.DB, pkg); err != nil {
				failed.Add(1)
				return nil
			}
			succeeded.Add(1)
			return nil
		})
	}
	_ = g.Wait()
	_ = packages.FinishSyncRun(ctx, application.DB, syncRun.RowID, "completed", map[string]any{
		"updated": succeeded.Load(), "failed": failed.Load(),
	})
	application.Logger.Info("dev: metadata fetched", "updated", succeeded.Load(), "failed", failed.Load())

	// 5. Build
	application.Logger.Info("dev: building repository")
	output := filepath.Join("storage", "repository", "builds")
	result, err := repository.Build(ctx, application.DB, repository.BuildOpts{
		OutputDir: output,
		AppURL:    application.Config.AppURL,
		Force:     true,
		Logger:    application.Logger,
	})
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	_, _ = application.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO builds (id, started_at, finished_at, duration_seconds,
			packages_total, packages_changed, packages_skipped,
			provider_groups, artifact_count, root_hash, sync_run_id, status, manifest_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.BuildID,
		result.StartedAt.Format(time.RFC3339),
		result.FinishedAt.Format(time.RFC3339),
		result.DurationSeconds,
		result.PackagesTotal, result.PackagesChanged, result.PackagesSkipped,
		result.ProviderGroups, result.ArtifactCount, result.RootHash,
		result.SyncRunID, "completed",
		fmt.Sprintf(`{"root_hash":"%s"}`, result.RootHash),
	)

	// 6. Deploy
	repoDir := filepath.Join("storage", "repository")
	if err := deploy.Promote(repoDir, result.BuildID, application.Logger); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}

	// 7. Serve
	application.Logger.Info("dev: ready",
		"url", fmt.Sprintf("http://localhost%s", application.Config.Server.Addr),
		"admin", "admin@localhost / admin",
		"packages", result.PackagesTotal,
	)

	return apphttp.ListenAndServe(application)
}

func init() {
	appCommand(devCmd)
	devCmd.Flags().String("addr", ":8080", "listen address")
	rootCmd.AddCommand(devCmd)
}
