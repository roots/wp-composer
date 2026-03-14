package cmd

import (
	"github.com/roots/wp-composer/internal/telemetry"
	"github.com/spf13/cobra"
)

var aggregateInstallsCmd = &cobra.Command{
	Use:   "aggregate-installs",
	Short: "Recompute install counters (total, 30d, last_installed_at)",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := telemetry.AggregateInstalls(cmd.Context(), application.DB)
		if err != nil {
			return err
		}
		application.Logger.Info("aggregation complete",
			"packages_updated", result.PackagesUpdated,
			"packages_reset", result.PackagesReset,
		)
		return nil
	},
}

func init() {
	appCommand(aggregateInstallsCmd)
	rootCmd.AddCommand(aggregateInstallsCmd)
}
