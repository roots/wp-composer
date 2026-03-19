package cmd

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/roots/wp-packages/internal/packages"
	"github.com/roots/wp-packages/internal/wporg"
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover packages from WordPress.org",
	Long:  "Creates shell package records (type, name, last_committed). Use 'update' to fetch full metadata.",
	RunE:  runDiscover,
}

func runDiscover(cmd *cobra.Command, args []string) error {
	source, _ := cmd.Flags().GetString("source")
	pkgType, _ := cmd.Flags().GetString("type")
	limit, _ := cmd.Flags().GetInt("limit")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	if concurrency <= 0 {
		concurrency = application.Config.Discovery.Concurrency
	}

	switch source {
	case "config":
		return discoverFromConfig(cmd.Context(), pkgType, limit, concurrency)
	case "svn":
		return discoverFromSVN(cmd.Context(), pkgType, limit)
	default:
		return fmt.Errorf("unknown source %q (use config or svn)", source)
	}
}

func discoverFromConfig(ctx context.Context, pkgType string, limit, concurrency int) error {
	seeds, err := packages.LoadSeeds(application.Config.Discovery.SeedsFile)
	if err != nil {
		return err
	}

	client := wporg.NewClient(application.Config.Discovery, application.Logger)

	type job struct {
		slug    string
		pkgType string
	}

	var jobs []job
	if pkgType == "all" || pkgType == "plugin" {
		for _, slug := range seeds.PopularPlugins {
			jobs = append(jobs, job{slug: slug, pkgType: "plugin"})
		}
	}
	if pkgType == "all" || pkgType == "theme" {
		for _, slug := range seeds.PopularThemes {
			jobs = append(jobs, job{slug: slug, pkgType: "theme"})
		}
	}

	if limit > 0 && limit < len(jobs) {
		jobs = jobs[:limit]
	}

	application.Logger.Info("discovering packages from config", "count", len(jobs), "concurrency", concurrency)

	var succeeded, failed atomic.Int64
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, j := range jobs {
		j := j
		g.Go(func() error {
			// Fetch minimal info from API to get last_updated date
			lastCommitted, fetchErr := client.FetchLastUpdated(gCtx, j.pkgType, j.slug)
			if fetchErr != nil {
				application.Logger.Warn("failed to fetch package info", "type", j.pkgType, "slug", j.slug, "error", fetchErr)
				failed.Add(1)
				return nil
			}

			if err := packages.UpsertShellPackage(gCtx, application.DB, j.pkgType, j.slug, lastCommitted); err != nil {
				application.Logger.Warn("failed to store shell package", "type", j.pkgType, "slug", j.slug, "error", err)
				failed.Add(1)
				return nil
			}

			succeeded.Add(1)
			application.Logger.Debug("discovered package", "type", j.pkgType, "slug", j.slug)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	application.Logger.Info("discovery complete", "succeeded", succeeded.Load(), "failed", failed.Load())
	if err := packages.RefreshSiteStats(ctx, application.DB); err != nil {
		return fmt.Errorf("refreshing package stats: %w", err)
	}
	if failed.Load() > 0 {
		return fmt.Errorf("discovery completed with %d failures", failed.Load())
	}
	return nil
}

func discoverFromSVN(ctx context.Context, pkgType string, limit int) error {
	const svnBatchSize = 500

	client := wporg.NewClient(application.Config.Discovery, application.Logger)

	type svnSource struct {
		url     string
		pkgType string
		metaKey string // site_meta key for storing last-seen SVN revision
	}

	var sources []svnSource
	if pkgType == "all" || pkgType == "plugin" {
		sources = append(sources, svnSource{
			url:     "https://plugins.svn.wordpress.org/",
			pkgType: "plugin",
			metaKey: "svn_revision_plugin",
		})
	}
	if pkgType == "all" || pkgType == "theme" {
		sources = append(sources, svnSource{
			url:     "https://themes.svn.wordpress.org/",
			pkgType: "theme",
			metaKey: "svn_revision_theme",
		})
	}

	var totalCount, totalFailed int
	for _, src := range sources {
		application.Logger.Info("discovering from SVN", "type", src.pkgType, "url", src.url)

		var count, failed int
		batch := make([]packages.ShellEntry, 0, svnBatchSize)

		flush := func() {
			if len(batch) == 0 {
				return
			}
			if err := packages.BatchUpsertShellPackages(ctx, application.DB, batch); err != nil {
				application.Logger.Warn("batch upsert failed, falling back to individual", "error", err)
				for _, e := range batch {
					if err := packages.UpsertShellPackage(ctx, application.DB, e.Type, e.Name, e.LastCommitted); err != nil {
						application.Logger.Warn("failed to upsert shell package", "slug", e.Name, "error", err)
						failed++
						count--
					}
				}
			}
			batch = batch[:0]
		}

		result, err := client.ParseSVNListing(ctx, src.url, func(entry wporg.SVNEntry) error {
			if limit > 0 && totalCount >= limit {
				return errLimitReached
			}

			batch = append(batch, packages.ShellEntry{
				Type:          src.pkgType,
				Name:          entry.Slug,
				LastCommitted: entry.LastCommitted,
			})
			count++
			totalCount++

			if len(batch) >= svnBatchSize {
				flush()
			}
			return nil
		})

		flush()

		if err != nil && err != errLimitReached {
			return fmt.Errorf("SVN discovery for %s: %w", src.pkgType, err)
		}

		// Use SVN revision log to find which packages changed since last run.
		// Skip when --limit is set (partial/test runs shouldn't mutate global
		// change state or trigger large update workloads).
		if limit == 0 && result != nil && result.Revision > 0 {
			if markErr := markChangedFromSVNLog(ctx, client, src, result.Revision); markErr != nil {
				application.Logger.Warn("SVN changelog fetch failed, skipping change detection",
					"type", src.pkgType, "error", markErr)
			}
		}

		totalFailed += failed
		application.Logger.Info("SVN discovery done", "type", src.pkgType, "succeeded", count, "failed", failed)

		if limit > 0 && totalCount >= limit {
			break
		}
	}

	if err := packages.RefreshSiteStats(ctx, application.DB); err != nil {
		return fmt.Errorf("refreshing package stats: %w", err)
	}

	if totalFailed > 0 {
		return fmt.Errorf("SVN discovery completed with %d failures", totalFailed)
	}
	return nil
}

// markChangedFromSVNLog fetches the SVN log between the last-seen revision and
// the current revision, extracts which slugs changed, and marks them in the DB
// so they'll be picked up by the update step.
func markChangedFromSVNLog(ctx context.Context, client *wporg.Client, src struct {
	url     string
	pkgType string
	metaKey string
}, currentRev int64) error {
	lastRevStr, err := packages.GetMeta(ctx, application.DB, src.metaKey)
	if err != nil {
		return fmt.Errorf("reading last revision: %w", err)
	}

	var lastRev int64
	if lastRevStr != "" {
		var parseErr error
		lastRev, parseErr = strconv.ParseInt(lastRevStr, 10, 64)
		if parseErr != nil {
			return fmt.Errorf("malformed stored revision %q for %s: %w", lastRevStr, src.metaKey, parseErr)
		}
	}

	if lastRev > 0 && lastRev < currentRev {
		application.Logger.Info("fetching SVN changelog",
			"type", src.pkgType, "from_rev", lastRev, "to_rev", currentRev)

		slugs, err := client.FetchSVNChangedSlugs(ctx, src.url, lastRev+1, currentRev)
		if err != nil {
			return err
		}

		if len(slugs) > 0 {
			affected, err := packages.MarkPackagesChanged(ctx, application.DB, src.pkgType, slugs)
			if err != nil {
				return fmt.Errorf("marking changed packages: %w", err)
			}
			application.Logger.Info("marked changed packages from SVN log",
				"type", src.pkgType, "slugs_in_log", len(slugs), "packages_marked", affected)
		}
	} else if lastRev == 0 {
		application.Logger.Info("no previous SVN revision stored, skipping changelog (first run)",
			"type", src.pkgType, "current_rev", currentRev)
	}

	// Store current revision for next run.
	if err := packages.SetMeta(ctx, application.DB, src.metaKey, strconv.FormatInt(currentRev, 10)); err != nil {
		return fmt.Errorf("storing revision: %w", err)
	}

	return nil
}

var errLimitReached = fmt.Errorf("limit reached")

func init() {
	appCommand(discoverCmd)
	discoverCmd.Flags().String("source", "config", "discovery source (config or svn)")
	discoverCmd.Flags().String("type", "all", "package type (plugin, theme, or all)")
	discoverCmd.Flags().Int("limit", 0, "maximum packages to discover (0 = unlimited)")
	discoverCmd.Flags().Int("concurrency", 0, "concurrent API requests (0 = use config default)")
	rootCmd.AddCommand(discoverCmd)
}
