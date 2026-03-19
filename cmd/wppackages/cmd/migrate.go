package cmd

import (
	"embed"

	"github.com/roots/wp-packages/internal/db"
	"github.com/spf13/cobra"
)

var Migrations embed.FS

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		application.Logger.Info("running migrations", "db", application.Config.DB.Path)
		if err := db.Migrate(application.DB, Migrations); err != nil {
			return err
		}
		application.Logger.Info("migrations complete")
		return nil
	},
}

func init() {
	appCommand(migrateCmd)
	rootCmd.AddCommand(migrateCmd)
}
