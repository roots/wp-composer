package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/roots/wp-composer/internal/config"
	"github.com/roots/wp-composer/internal/db"
	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore database from Litestream backup on R2",
	RunE:  runDBRestore,
}

func runDBRestore(cmd *cobra.Command, args []string) error {
	// Load config manually — we don't want the app to open the DB.
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if dbPath != "" {
		cfg.DB.Path = dbPath
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "" {
		output = cfg.DB.Path
	}

	force, _ := cmd.Flags().GetBool("force")

	// Check if target file already exists.
	if _, err := os.Stat(output); err == nil && !force {
		return fmt.Errorf("database file %s already exists (use --force to overwrite)", output)
	}

	// Verify litestream is installed.
	litestreamPath, err := exec.LookPath("litestream")
	if err != nil {
		return fmt.Errorf("litestream binary not found in PATH: %w", err)
	}

	// Build the replica URL.
	bucket := cfg.Litestream.Bucket
	if bucket == "" {
		return fmt.Errorf("LITESTREAM_BUCKET is not set")
	}
	path := cfg.Litestream.Path
	if path == "" {
		path = "db"
	}
	endpoint := cfg.R2.Endpoint
	if endpoint == "" {
		return fmt.Errorf("R2_ENDPOINT is not set")
	}

	replicaURL := fmt.Sprintf("s3://%s/%s", bucket, path)

	// Run litestream restore.
	restoreCmd := exec.CommandContext(cmd.Context(), litestreamPath, "restore", "-o", output, replicaURL)
	restoreCmd.Env = append(os.Environ(),
		"LITESTREAM_ACCESS_KEY_ID="+cfg.R2.AccessKeyID,
		"LITESTREAM_SECRET_ACCESS_KEY="+cfg.R2.SecretAccessKey,
		"LITESTREAM_ENDPOINT="+endpoint,
	)
	restoreCmd.Stdout = os.Stdout
	restoreCmd.Stderr = os.Stderr

	fmt.Printf("Restoring database to %s...\n", output)
	if err := restoreCmd.Run(); err != nil {
		return fmt.Errorf("litestream restore failed: %w", err)
	}

	// Run migrations to ensure schema is up to date.
	fmt.Println("Running migrations...")
	sqlDB, err := db.Open(output)
	if err != nil {
		return fmt.Errorf("opening restored database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := db.Migrate(sqlDB, Migrations); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	fmt.Println("Restore complete.")
	return nil
}

func init() {
	dbRestoreCmd.Flags().StringP("output", "o", "", "output path for restored database (default: config DB path)")
	dbRestoreCmd.Flags().Bool("force", false, "overwrite existing database file")
	dbCmd.AddCommand(dbRestoreCmd)
	rootCmd.AddCommand(dbCmd)
}
