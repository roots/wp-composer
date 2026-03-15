package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/roots/wp-composer/internal/deploy"
	"github.com/spf13/cobra"
)

const pipelineLockPath = "storage/pipeline.lock"

// pipelineLockFile holds the lock file reference for the lifetime of the process,
// preventing the GC finalizer from closing the fd and releasing the lock.
var pipelineLockFile *os.File

// acquirePipelineLock ensures only one pipeline runs at a time.
func acquirePipelineLock() error {
	return acquireLock(pipelineLockPath)
}

// acquireLock acquires an exclusive file lock at lockPath. If PIPELINE_LOCK_FD is
// set, the lock is expected to have been inherited from the parent process via fd
// passing; the inherited fd is validated for both inode identity and lock ownership.
func acquireLock(lockPath string) error {
	if v := os.Getenv("PIPELINE_LOCK_FD"); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid PIPELINE_LOCK_FD: %w", err)
		}
		pipelineLockFile = os.NewFile(uintptr(fd), lockPath)
		if pipelineLockFile == nil {
			return fmt.Errorf("PIPELINE_LOCK_FD=%d: invalid file descriptor", fd)
		}

		// Verify the inherited fd points to the actual lock file (inode match).
		var fdStat, pathStat syscall.Stat_t
		if err := syscall.Fstat(fd, &fdStat); err != nil {
			return fmt.Errorf("PIPELINE_LOCK_FD=%d: fstat failed: %w", fd, err)
		}
		if err := syscall.Stat(lockPath, &pathStat); err != nil {
			return fmt.Errorf("pipeline lock stat: %w", err)
		}
		if fdStat.Dev != pathStat.Dev || fdStat.Ino != pathStat.Ino {
			return fmt.Errorf("PIPELINE_LOCK_FD=%d does not refer to %s", fd, lockPath)
		}

		// Verify this fd actually holds the lock. Re-locking our own fd is a
		// no-op in flock, so LOCK_EX|LOCK_NB succeeds if we hold it, and fails
		// with EWOULDBLOCK if a different open file description holds it.
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return fmt.Errorf("PIPELINE_LOCK_FD=%d does not hold the pipeline lock", fd)
		}
		return nil
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("pipeline lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return fmt.Errorf("pipeline already running (could not acquire %s)", lockPath)
	}
	pipelineLockFile = f
	return nil
}

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run discover → update → build → deploy",
	RunE:  runPipeline,
}

// pipelineBuildID is set by runPipeline so that runBuild can UPDATE the
// existing "running" row instead of INSERTing a new one.
var pipelineBuildID string

func runPipeline(cmd *cobra.Command, args []string) error {
	// Acquire a system-wide file lock so only one pipeline runs at a time.
	// When triggered from the admin UI, the parent process passes the already-locked
	// fd via PIPELINE_LOCK_FD so the lock transfers atomically with no TOCTOU gap.
	if err := acquirePipelineLock(); err != nil {
		return err
	}

	skipDiscover, _ := cmd.Flags().GetBool("skip-discover")
	skipDeploy, _ := cmd.Flags().GetBool("skip-deploy")
	discoverSource, _ := cmd.Flags().GetString("discover-source")

	ctx := cmd.Context()
	started := time.Now().UTC()

	// Mark any stale "running" builds (dead PID) as cancelled.
	markStaleBuildsCancelled(ctx, application.DB)

	// Record this build as "running" before doing any work.
	buildID := started.Format("20060102-150405")
	pipelineBuildID = buildID
	defer func() { pipelineBuildID = "" }()
	_, err := application.DB.ExecContext(ctx, `
		INSERT INTO builds (id, started_at, status, pid,
			packages_total, packages_changed, packages_skipped,
			provider_groups, artifact_count, root_hash, manifest_json)
		VALUES (?, ?, 'running', ?, 0, 0, 0, 0, 0, '', '{}')`,
		buildID,
		started.Format(time.RFC3339),
		os.Getpid(),
	)
	if err != nil {
		return fmt.Errorf("recording running build: %w", err)
	}

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

		// Clean up old builds after a successful deploy.
		repoDir := filepath.Join("storage", "repository")
		if removed, err := deploy.Cleanup(repoDir, 5, application.Logger); err != nil {
			application.Logger.Warn("pipeline: local cleanup failed", "error", err)
		} else if removed > 0 {
			application.Logger.Info("pipeline: local cleanup done", "removed", removed)
		}

		if application.Config.R2.Enabled {
			if removed, err := deploy.CleanupR2(ctx, application.Config.R2, 24, 5, application.Logger); err != nil {
				application.Logger.Warn("pipeline: R2 cleanup failed", "error", err)
			} else if removed > 0 {
				application.Logger.Info("pipeline: R2 cleanup done", "objects_removed", removed)
			}
		}
	}

	return nil
}

func recordFailedBuild(cmd *cobra.Command, started time.Time, pipelineErr error) {
	now := time.Now().UTC()
	_, dbErr := application.DB.ExecContext(cmd.Context(), `
		UPDATE builds SET status = 'failed', finished_at = ?, duration_seconds = ?, error_message = ?
		WHERE id = ?`,
		now.Format(time.RFC3339),
		int(now.Sub(started).Seconds()),
		pipelineErr.Error(),
		pipelineBuildID,
	)
	if dbErr != nil {
		application.Logger.Warn("failed to record failed build in database", "error", dbErr)
	}
}

// markStaleBuildsCancelled finds builds with status "running" whose PID is no
// longer alive and marks them as "cancelled".
func markStaleBuildsCancelled(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `SELECT id, pid FROM builds WHERE status = 'running' AND pid IS NOT NULL`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id string
		var pid int
		if err := rows.Scan(&id, &pid); err != nil {
			continue
		}
		// Signal 0 checks if the process exists without sending a real signal.
		// ESRCH = no such process (safe to cancel). EPERM = process exists but
		// we lack permission (do not cancel). Any other error is unexpected.
		if err := syscall.Kill(pid, 0); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				_, _ = db.ExecContext(ctx, `UPDATE builds SET status = 'cancelled', finished_at = ? WHERE id = ?`,
					time.Now().UTC().Format(time.RFC3339), id)
			} else {
				application.Logger.Warn("stale build check: unexpected kill(0) error", "build_id", id, "pid", pid, "error", err)
			}
		}
	}
}

func init() {
	appCommand(pipelineCmd)
	pipelineCmd.Flags().String("discover-source", "config", "discovery source (config or svn)")
	pipelineCmd.Flags().Bool("skip-discover", false, "skip the discover step")
	pipelineCmd.Flags().Bool("skip-deploy", false, "skip the deploy step")
	rootCmd.AddCommand(pipelineCmd)
}
