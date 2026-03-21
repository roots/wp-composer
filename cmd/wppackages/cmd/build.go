package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/roots/wp-packages/internal/deploy"
	"github.com/roots/wp-packages/internal/repository"
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

	// Resolve previous build for change detection
	repoDir := filepath.Dir(output) // storage/repository
	prevBuildDir := ""
	if id, err := deploy.CurrentBuildID(repoDir); err == nil && id != "" {
		prevBuildDir = deploy.BuildDirFromID(repoDir, id)
	}

	result, err := repository.Build(cmd.Context(), application.DB, repository.BuildOpts{
		OutputDir:        output,
		AppURL:           application.Config.AppURL,
		Force:            force,
		PackageName:      pkg,
		BuildID:          pipelineBuildID,
		PreviousBuildDir: prevBuildDir,
		Logger:           application.Logger,
	})
	if err != nil {
		return err
	}

	// Record build in database. When running inside a pipeline, the row already
	// exists with status "running" — update it. Otherwise insert a new row.
	var dbErr error
	if pipelineBuildID != "" {
		// Only write build metrics here. Step durations are written by
		// runPipeline after all steps (including deploy) have completed.
		_, dbErr = application.DB.ExecContext(cmd.Context(), `
			UPDATE builds SET
				packages_total = ?, packages_changed = ?, packages_skipped = ?,
				artifact_count = ?, root_hash = ?,
				sync_run_id = ?, manifest_json = ?
			WHERE id = ?`,
			result.PackagesTotal,
			result.PackagesChanged,
			result.PackagesSkipped,
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
				artifact_count, root_hash, sync_run_id, status, manifest_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			result.BuildID,
			result.StartedAt.Format(time.RFC3339),
			result.FinishedAt.Format(time.RFC3339),
			result.DurationSeconds,
			result.PackagesTotal,
			result.PackagesChanged,
			result.PackagesSkipped,
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

	// Record metadata changes for the changes feed
	if len(result.ChangedPackages) > 0 {
		if err := persistMetadataChanges(cmd.Context(), result); err != nil {
			application.Logger.Warn("failed to record metadata changes", "error", err)
		}
	}

	// Cleanup: delete changes older than 24 hours (runs every build regardless of changes)
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	if _, err := application.DB.ExecContext(cmd.Context(),
		`DELETE FROM metadata_changes WHERE timestamp < ?`, cutoff); err != nil {
		application.Logger.Warn("failed to cleanup old metadata changes", "error", err)
	}

	return nil
}

func persistMetadataChanges(ctx context.Context, result *repository.BuildResult) error {
	buildTimestamp := result.FinishedAt.UnixMilli()
	tx, err := application.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO metadata_changes (package_name, action, timestamp, build_id)
		 VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, c := range result.ChangedPackages {
		if _, err := stmt.ExecContext(ctx, c.Name, c.Action, buildTimestamp, result.BuildID); err != nil {
			return fmt.Errorf("insert %s: %w", c.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
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
