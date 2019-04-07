package cmd

import "github.com/spf13/cobra"

// allCmd represents the all command
var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Starts all estimations",
	Long:  `Starts all estimations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.Info("Starting all services")
		go naiveCommand.RunE(naiveCommand, args)
		go coreCommand.RunE(coreCommand, args)
		go mempoolCommand.RunE(mempoolCommand, args)
		return btcutilCommand.RunE(btcutilCommand, args)
	},
}

func init() {
	RootCmd.AddCommand(allCmd)
}
