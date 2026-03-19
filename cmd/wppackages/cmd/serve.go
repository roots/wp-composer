package cmd

import (
	apphttp "github.com/roots/wp-packages/internal/http"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if addr, _ := cmd.Flags().GetString("addr"); addr != "" {
			application.Config.Server.Addr = addr
		}
		return apphttp.ListenAndServe(application)
	},
}

func init() {
	appCommand(serveCmd)
	serveCmd.Flags().String("addr", "", "listen address (default :8080)")
	rootCmd.AddCommand(serveCmd)
}
