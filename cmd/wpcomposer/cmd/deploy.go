package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/roots/wp-composer/internal/deploy"
	"github.com/spf13/cobra"
)

// deployR2SyncSeconds captures the last SyncToR2 duration for the current deploy run.
var deployR2SyncSeconds *int

var deployCmd = &cobra.Command{
	Use:   "deploy [build-id]",
	Short: "Promote a build, rollback, or cleanup old builds",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeploy,
}

func secondsPtrSince(start time.Time) *int {
	v := int(time.Since(start).Seconds())
	return &v
}

func syncToR2Timed(cmd *cobra.Command, buildDir, buildID, previousBuildDir string) error {
	started := time.Now()
	err := deploy.SyncToR2(cmd.Context(), application.Config.R2, buildDir, buildID, previousBuildDir, application.Logger)
	deployR2SyncSeconds = secondsPtrSince(started)
	if err != nil {
		return fmt.Errorf("R2 sync failed: %w", err)
	}
	recordR2Sync(cmd, buildID)
	return nil
}

func runDeploy(cmd *cobra.Command, args []string) error {
	deployR2SyncSeconds = nil

	repoDir := filepath.Join("storage", "repository")
	cleanup, _ := cmd.Flags().GetBool("cleanup")
	toR2, _ := cmd.Flags().GetBool("to-r2")
	previousBuildDir := previousBuildDirFor(repoDir)

	r2Cleanup, _ := cmd.Flags().GetBool("r2-cleanup")
	retainCount, _ := cmd.Flags().GetInt("retain")
	graceHours, _ := cmd.Flags().GetInt("grace-hours")

	if cleanup {
		removed, err := deploy.Cleanup(repoDir, retainCount, application.Logger)
		if err != nil {
			return err
		}
		if removed == 0 {
			application.Logger.Info("nothing to clean up locally")
		}

		if r2Cleanup {
			r2Removed, err := deploy.CleanupR2(cmd.Context(), application.Config.R2, graceHours, retainCount, application.Logger)
			if err != nil {
				return fmt.Errorf("R2 cleanup failed: %w", err)
			}
			if r2Removed == 0 {
				application.Logger.Info("nothing to clean up on R2")
			}
		}
		return nil
	}

	if cmd.Flags().Changed("rollback") {
		rollbackVal, _ := cmd.Flags().GetString("rollback")
		target := strings.TrimSpace(rollbackVal)

		// Resolve the rollback target before syncing to R2
		if target == "" {
			currentID, _ := deploy.CurrentBuildID(repoDir)
			builds, err := deploy.ListBuilds(repoDir)
			if err != nil {
				return err
			}
			for i := len(builds) - 1; i >= 0; i-- {
				if builds[i] != currentID {
					target = builds[i]
					break
				}
			}
			if target == "" {
				return fmt.Errorf("no previous build available for rollback")
			}
		}

		// Validate build before any sync or promote
		buildDir := deploy.BuildDirFromID(repoDir, target)
		if err := deploy.ValidateBuild(buildDir); err != nil {
			return fmt.Errorf("invalid build %s: %w", target, err)
		}

		// Sync to R2 first, then promote locally
		if toR2 || application.Config.R2.Enabled {
			if err := syncToR2Timed(cmd, buildDir, target, previousBuildDir); err != nil {
				return err
			}
		}

		if _, err := deploy.Rollback(repoDir, target, application.Logger); err != nil {
			return err
		}
		return nil
	}

	// Promote
	var buildID string
	if len(args) > 0 {
		buildID = args[0]
	} else {
		var err error
		buildID, err = deploy.LatestBuildID(repoDir)
		if err != nil {
			return err
		}
	}

	// Validate build before any sync or promote
	buildDir := deploy.BuildDirFromID(repoDir, buildID)
	if err := deploy.ValidateBuild(buildDir); err != nil {
		return fmt.Errorf("invalid build %s: %w", buildID, err)
	}

	// Sync to R2 first, then promote locally
	if toR2 || application.Config.R2.Enabled {
		if err := syncToR2Timed(cmd, buildDir, buildID, previousBuildDir); err != nil {
			return err
		}
	}

	if err := deploy.Promote(repoDir, buildID, application.Logger); err != nil {
		return err
	}

	return nil
}

// previousBuildDirFor returns the build directory for the currently promoted
// build, but only if that build was previously synced to R2 (has r2_synced_at).
// Returns "" if no build is promoted or the promoted build was never R2-synced.
// This prevents skipping shared p/ uploads for builds that only exist locally.
func previousBuildDirFor(repoDir string) string {
	id, _ := deploy.CurrentBuildID(repoDir)
	if id == "" {
		return ""
	}
	var synced *string
	err := application.DB.QueryRow(
		`SELECT r2_synced_at FROM builds WHERE id = ?`, id,
	).Scan(&synced)
	if err != nil || synced == nil {
		return ""
	}
	return deploy.BuildDirFromID(repoDir, id)
}

func recordR2Sync(cmd *cobra.Command, buildID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := application.DB.ExecContext(cmd.Context(),
		`UPDATE builds SET r2_synced_at = ? WHERE id = ?`, now, buildID)
	if err != nil {
		application.Logger.Warn("failed to record R2 sync timestamp", "build_id", buildID, "error", err)
	}
}

func init() {
	appCommand(deployCmd)
	f := deployCmd.Flags()
	f.String("rollback", "", "rollback to previous build, or specify a build ID")
	f.Lookup("rollback").NoOptDefVal = " " // allows --rollback without =value
	f.Bool("cleanup", false, "remove old builds beyond retention")
	f.Bool("r2-cleanup", false, "also clean stale files from R2 during cleanup")
	f.Int("retain", 5, "number of recent builds to retain (beyond current)")
	f.Int("grace-hours", 24, "hours to keep old releases on R2 after deploy")
	f.Bool("to-r2", false, "sync build to R2")
	rootCmd.AddCommand(deployCmd)
}
