package cmd

import (
	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run discover → update → build → deploy",
	RunE:  runPipeline,
}

func runPipeline(cmd *cobra.Command, args []string) error {
	skipDiscover, _ := cmd.Flags().GetBool("skip-discover")
	skipDeploy, _ := cmd.Flags().GetBool("skip-deploy")
	discoverSource, _ := cmd.Flags().GetString("discover-source")

	if !skipDiscover {
		application.Logger.Info("pipeline: running discover")
		_ = discoverCmd.Flags().Set("source", discoverSource)
		if err := runDiscover(cmd, nil); err != nil {
			return err
		}
	}

	application.Logger.Info("pipeline: running update")
	if err := runUpdate(cmd, nil); err != nil {
		return err
	}

	application.Logger.Info("pipeline: running build")
	if err := runBuild(cmd, nil); err != nil {
		return err
	}

	if !skipDeploy {
		application.Logger.Info("pipeline: running deploy")
		if err := runDeploy(cmd, nil); err != nil {
			return err
		}
	}

	application.Logger.Info("pipeline: complete")
	return nil
}

func init() {
	appCommand(pipelineCmd)
	pipelineCmd.Flags().String("discover-source", "config", "discovery source (config or svn)")
	pipelineCmd.Flags().Bool("skip-discover", false, "skip the discover step")
	pipelineCmd.Flags().Bool("skip-deploy", false, "skip the deploy step")
	rootCmd.AddCommand(pipelineCmd)
}
