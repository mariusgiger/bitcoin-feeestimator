package cmd

import (
	"github.com/spf13/cobra"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate/core"
)

// naiveCommand represents the command for naive btc estimation
var coreCommand = &cobra.Command{
	Use:   "core",
	Short: "Runs core fee estimation",
	Long:  `Runs core fee estimation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		estimator := core.NewRPCEstimator(logger, client, rateCache)
		return estimator.Run()
	},
}

func init() {
	RootCmd.AddCommand(coreCommand)
}
