package cmd

import (
	"github.com/spf13/cobra"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate/naive"
)

// naiveCommand represents the command for naive btc estimation
var naiveCommand = &cobra.Command{
	Use:   "naive",
	Short: "Runs naive fee estimation",
	Long:  `Runs naive fee estimation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		estimator := naive.NewEstimator(logger, client, rateCache)
		return estimator.Run()
	},
}

func init() {
	RootCmd.AddCommand(naiveCommand)
}
