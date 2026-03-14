package cmd

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/roots/wp-composer/internal/packages"
	"github.com/roots/wp-composer/internal/wporg"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Fetch and update package metadata from WordPress.org",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	pkgType, _ := cmd.Flags().GetString("type")
	name, _ := cmd.Flags().GetString("name")
	force, _ := cmd.Flags().GetBool("force")
	limit, _ := cmd.Flags().GetInt("limit")
	includeInactive, _ := cmd.Flags().GetBool("include-inactive")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	if concurrency <= 0 {
		concurrency = application.Config.Discovery.Concurrency
	}

	ctx := cmd.Context()

	syncRun, err := packages.AllocateSyncRunID(ctx, application.DB)
	if err != nil {
		return fmt.Errorf("allocating sync run: %w", err)
	}
	application.Logger.Info("starting update", "sync_run_id", syncRun.RunID)

	pkgs, err := packages.GetPackagesNeedingUpdate(ctx, application.DB, packages.UpdateQueryOpts{
		Type:            pkgType,
		Name:            name,
		Force:           force,
		IncludeInactive: includeInactive,
		Limit:           limit,
	})
	if err != nil {
		return fmt.Errorf("querying packages: %w", err)
	}

	if len(pkgs) == 0 {
		application.Logger.Info("no packages need updating")
		if err := packages.FinishSyncRun(ctx, application.DB, syncRun.RowID, "completed", map[string]any{"updated": 0}); err != nil {
			return fmt.Errorf("finishing sync run: %w", err)
		}
		return nil
	}

	application.Logger.Info("updating packages", "count", len(pkgs), "concurrency", concurrency)

	client := wporg.NewClient(application.Config.Discovery, application.Logger)

	var succeeded, failed, deactivated atomic.Int64
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
				application.Logger.Warn("failed to fetch", "type", p.Type, "name", p.Name, "error", fetchErr)
				failed.Add(1)
				return nil
			}

			pkg := packages.PackageFromAPIData(data, p.Type)
			pkg.ID = p.ID

			validVersions, err := pkg.NormalizeAndStoreVersions()
			if err != nil {
				application.Logger.Warn("version normalization failed", "type", p.Type, "name", p.Name, "error", err)
				failed.Add(1)
				return nil
			}

			if validVersions == 0 {
				pkg.IsActive = false
				if err := packages.DeactivatePackage(gCtx, application.DB, p.ID); err != nil {
					application.Logger.Warn("failed to deactivate", "type", p.Type, "name", p.Name, "error", err)
				}
				deactivated.Add(1)
			}

			now := time.Now().UTC()
			pkg.LastSyncedAt = &now
			pkg.LastSyncRunID = &syncRun.RunID

			if err := packages.UpsertPackage(gCtx, application.DB, pkg); err != nil {
				application.Logger.Warn("failed to store", "type", p.Type, "name", p.Name, "error", err)
				failed.Add(1)
				return nil
			}

			succeeded.Add(1)
			application.Logger.Debug("updated package", "type", p.Type, "name", p.Name, "versions", validVersions)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	stats := map[string]any{
		"updated":     succeeded.Load(),
		"failed":      failed.Load(),
		"deactivated": deactivated.Load(),
	}

	status := "completed"
	if failed.Load() > 0 {
		status = "completed_with_errors"
	}

	if err := packages.FinishSyncRun(ctx, application.DB, syncRun.RowID, status, stats); err != nil {
		return fmt.Errorf("finishing sync run: %w", err)
	}

	application.Logger.Info("update complete",
		"updated", succeeded.Load(),
		"failed", failed.Load(),
		"deactivated", deactivated.Load(),
	)
	return nil
}

func init() {
	appCommand(updateCmd)
	updateCmd.Flags().String("type", "all", "package type (plugin, theme, or all)")
	updateCmd.Flags().String("name", "", "specific package slug to update")
	updateCmd.Flags().Bool("force", false, "force update all packages")
	updateCmd.Flags().Int("limit", 0, "maximum packages to update (0 = unlimited)")
	updateCmd.Flags().Bool("include-inactive", false, "include inactive packages")
	updateCmd.Flags().Int("concurrency", 0, "concurrent API requests (0 = use config default)")
	rootCmd.AddCommand(updateCmd)
}
