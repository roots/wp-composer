package cmd

import (
	"fmt"

	"github.com/roots/wp-composer/internal/app"
	"github.com/roots/wp-composer/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	dbPath   string
	logLevel string

	application *app.App
)

var rootCmd = &cobra.Command{
	Use:   "wpcomposer",
	Short: "WP Composer - Composer repository for WordPress packages",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations["requires_app"] != "true" {
			return nil
		}

		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if dbPath != "" {
			cfg.DB.Path = dbPath
		}
		if logLevel != "" {
			cfg.LogLevel = logLevel
		}

		application, err = app.New(cfg)
		if err != nil {
			return fmt.Errorf("initializing app: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if application != nil {
			_ = application.Close()
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "database path (default ./storage/wpcomposer.db)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level (debug, info, warn, error)")
}

// appCommand sets the annotation that triggers app initialization in PersistentPreRunE.
func appCommand(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["requires_app"] = "true"
}
