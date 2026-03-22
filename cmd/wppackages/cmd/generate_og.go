package cmd

import (
	"github.com/roots/wp-packages/internal/og"
	"github.com/spf13/cobra"
)

var generateOGCmd = &cobra.Command{
	Use:   "generate-og",
	Short: "Generate OG images for packages that need them",
	RunE: func(cmd *cobra.Command, args []string) error {
		uploader := og.NewUploader(application.Config.R2)

		result, err := og.GenerateAll(cmd.Context(), application.DB, uploader, application.Logger)
		if err != nil {
			return err
		}

		application.Logger.Info("OG generation complete",
			"generated", result.Generated,
			"skipped", result.Skipped,
			"errors", result.Errors,
		)
		return nil
	},
}

func init() {
	appCommand(generateOGCmd)
	rootCmd.AddCommand(generateOGCmd)
}
