package cmd

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/roots/wp-composer/internal/packages"
	"github.com/roots/wp-composer/internal/wporg"
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
	if failed.Load() > 0 {
		return fmt.Errorf("discovery completed with %d failures", failed.Load())
	}
	return nil
}

func discoverFromSVN(ctx context.Context, pkgType string, limit int) error {
	client := wporg.NewClient(application.Config.Discovery, application.Logger)

	type svnSource struct {
		url     string
		pkgType string
	}

	var sources []svnSource
	if pkgType == "all" || pkgType == "plugin" {
		sources = append(sources, svnSource{url: "https://plugins.svn.wordpress.org/", pkgType: "plugin"})
	}
	if pkgType == "all" || pkgType == "theme" {
		sources = append(sources, svnSource{url: "https://themes.svn.wordpress.org/", pkgType: "theme"})
	}

	var totalCount, totalFailed int
	for _, src := range sources {
		application.Logger.Info("discovering from SVN", "type", src.pkgType, "url", src.url)

		var count, failed int
		err := client.ParseSVNListing(ctx, src.url, func(entry wporg.SVNEntry) error {
			if limit > 0 && totalCount >= limit {
				return errLimitReached
			}

			if err := packages.UpsertShellPackage(ctx, application.DB, src.pkgType, entry.Slug, entry.LastCommitted); err != nil {
				application.Logger.Warn("failed to upsert shell package", "slug", entry.Slug, "error", err)
				failed++
				return nil
			}
			count++
			totalCount++
			return nil
		})
		if err != nil && err != errLimitReached {
			return fmt.Errorf("SVN discovery for %s: %w", src.pkgType, err)
		}

		totalFailed += failed
		application.Logger.Info("SVN discovery done", "type", src.pkgType, "succeeded", count, "failed", failed)

		if limit > 0 && totalCount >= limit {
			break
		}
	}

	if totalFailed > 0 {
		return fmt.Errorf("SVN discovery completed with %d failures", totalFailed)
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
