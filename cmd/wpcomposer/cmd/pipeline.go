package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run discover → update → build → deploy",
	RunE:  runPipeline,
}

func runPipeline(cmd *cobra.Command, args []string) error {
	skipDiscover, _ := cmd.Flags().GetBool("skip-discover")
	skipDeploy, _ := cmd.Flags().GetBool("skip-deploy")
	discoverSource, _ := cmd.Flags().GetString("discover-source")

	ctx := cmd.Context()
	started := time.Now().UTC()

	if err := executePipelineSteps(cmd, ctx, skipDiscover, skipDeploy, discoverSource); err != nil {
		recordFailedBuild(cmd, started, err)
		return err
	}

	application.Logger.Info("pipeline: complete")
	return nil
}

func executePipelineSteps(cmd *cobra.Command, ctx context.Context, skipDiscover, skipDeploy bool, discoverSource string) error {
	if !skipDiscover {
		application.Logger.Info("pipeline: running discover")
		discoverCmd.SetContext(ctx)
		_ = discoverCmd.Flags().Set("source", discoverSource)
		if err := runDiscover(discoverCmd, nil); err != nil {
			return fmt.Errorf("discover: %w", err)
		}
	}

	application.Logger.Info("pipeline: running update")
	updateCmd.SetContext(ctx)
	if err := runUpdate(updateCmd, nil); err != nil {
		return fmt.Errorf("update: %w", err)
	}

	application.Logger.Info("pipeline: running build")
	buildCmd.SetContext(ctx)
	if err := runBuild(buildCmd, nil); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	if !skipDeploy {
		application.Logger.Info("pipeline: running deploy")
		deployCmd.SetContext(ctx)
		if err := runDeploy(deployCmd, nil); err != nil {
			return fmt.Errorf("deploy: %w", err)
		}
	}

	return nil
}

func recordFailedBuild(cmd *cobra.Command, started time.Time, pipelineErr error) {
	now := time.Now().UTC()
	buildID := started.Format("20060102-150405")
	_, dbErr := application.DB.ExecContext(cmd.Context(), `
		INSERT INTO builds (id, started_at, finished_at, duration_seconds,
			packages_total, packages_changed, packages_skipped,
			provider_groups, artifact_count, root_hash, sync_run_id, status, manifest_json, error_message)
		VALUES (?, ?, ?, ?, 0, 0, 0, 0, 0, '', NULL, 'failed', '{}', ?)`,
		buildID,
		started.Format(time.RFC3339),
		now.Format(time.RFC3339),
		int(now.Sub(started).Seconds()),
		pipelineErr.Error(),
	)
	if dbErr != nil {
		application.Logger.Warn("failed to record failed build in database", "error", dbErr)
	}
}

func init() {
	appCommand(pipelineCmd)
	pipelineCmd.Flags().String("discover-source", "config", "discovery source (config or svn)")
	pipelineCmd.Flags().Bool("skip-discover", false, "skip the discover step")
	pipelineCmd.Flags().Bool("skip-deploy", false, "skip the deploy step")
	rootCmd.AddCommand(pipelineCmd)
}
