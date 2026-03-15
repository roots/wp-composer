package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/roots/wp-composer/internal/deploy"
	"github.com/roots/wp-composer/internal/repository"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Generate Composer repository artifacts",
	RunE:  runBuild,
}

func runBuild(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	pkg, _ := cmd.Flags().GetString("package")
	output, _ := cmd.Flags().GetString("output")

	if output == "" {
		output = filepath.Join("storage", "repository", "builds")
	}

	// Resolve previous build dir for incremental builds
	var previousBuildDir string
	repoDir := filepath.Dir(output) // storage/repository
	if latestID, err := deploy.LatestBuildID(repoDir); err == nil && latestID != "" {
		candidate := deploy.BuildDirFromID(repoDir, latestID)
		if err := deploy.ValidateBuild(candidate); err == nil {
			previousBuildDir = candidate
		}
	}

	result, err := repository.Build(cmd.Context(), application.DB, repository.BuildOpts{
		OutputDir:        output,
		AppURL:           application.Config.AppURL,
		Force:            force,
		PackageName:      pkg,
		PreviousBuildDir: previousBuildDir,
		BuildID:          pipelineBuildID,
		Logger:           application.Logger,
	})
	if err != nil {
		return err
	}

	// Record build in database. When running inside a pipeline, the row already
	// exists with status "running" — update it. Otherwise insert a new row.
	var dbErr error
	if pipelineBuildID != "" {
		_, dbErr = application.DB.ExecContext(cmd.Context(), `
			UPDATE builds SET finished_at = ?, duration_seconds = ?,
				packages_total = ?, packages_changed = ?, packages_skipped = ?,
				provider_groups = ?, artifact_count = ?, root_hash = ?,
				sync_run_id = ?, status = 'completed',
				manifest_json = ?
			WHERE id = ?`,
			result.FinishedAt.Format(time.RFC3339),
			result.DurationSeconds,
			result.PackagesTotal,
			result.PackagesChanged,
			result.PackagesSkipped,
			result.ProviderGroups,
			result.ArtifactCount,
			result.RootHash,
			result.SyncRunID,
			fmt.Sprintf(`{"root_hash":"%s"}`, result.RootHash),
			pipelineBuildID,
		)
	} else {
		_, dbErr = application.DB.ExecContext(cmd.Context(), `
			INSERT INTO builds (id, started_at, finished_at, duration_seconds,
				packages_total, packages_changed, packages_skipped,
				provider_groups, artifact_count, root_hash, sync_run_id, status, manifest_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			result.BuildID,
			result.StartedAt.Format(time.RFC3339),
			result.FinishedAt.Format(time.RFC3339),
			result.DurationSeconds,
			result.PackagesTotal,
			result.PackagesChanged,
			result.PackagesSkipped,
			result.ProviderGroups,
			result.ArtifactCount,
			result.RootHash,
			result.SyncRunID,
			"completed",
			fmt.Sprintf(`{"root_hash":"%s"}`, result.RootHash),
		)
	}
	if dbErr != nil {
		application.Logger.Warn("failed to record build in database", "error", dbErr)
	}

	return nil
}

func init() {
	appCommand(buildCmd)
	buildCmd.Flags().Bool("force", false, "rebuild all packages")
	buildCmd.Flags().String("package", "", "build single package (e.g. wp-plugin/akismet)")
	buildCmd.Flags().String("output", "", "output directory (default storage/repository/builds)")
	rootCmd.AddCommand(buildCmd)
}
