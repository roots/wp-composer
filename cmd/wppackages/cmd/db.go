package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/roots/wp-packages/internal/config"
	"github.com/roots/wp-packages/internal/db"
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

	// Locate litestream config file (next to the binary or in working dir).
	litestreamConfig := "litestream.yml"
	if _, err := os.Stat(litestreamConfig); err != nil {
		return fmt.Errorf("litestream.yml not found in working directory")
	}

	dbPath := cfg.DB.Path

	// Run litestream restore.
	restoreCmd := exec.CommandContext(cmd.Context(), litestreamPath, "restore", "-config", litestreamConfig, "-o", output, dbPath)
	restoreCmd.Env = os.Environ()
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
