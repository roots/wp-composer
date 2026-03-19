package cmd

import (
	"github.com/roots/wp-packages/internal/auth"
	"github.com/spf13/cobra"
)

var cleanupSessionsCmd = &cobra.Command{
	Use:   "cleanup-sessions",
	Short: "Delete expired sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		deleted, err := auth.CleanupExpiredSessions(cmd.Context(), application.DB)
		if err != nil {
			return err
		}
		application.Logger.Info("expired sessions cleaned up", "deleted", deleted)
		return nil
	},
}

func init() {
	appCommand(cleanupSessionsCmd)
	rootCmd.AddCommand(cleanupSessionsCmd)
}
