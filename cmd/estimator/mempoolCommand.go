package cmd

import (
	"github.com/spf13/cobra"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate/mempool"
)

// mempoolCommand represents the command for mempool estimation
var mempoolCommand = &cobra.Command{
	Use:   "mempool",
	Short: "Runs the mempool fee estimation",
	Long:  `Runs the mempool fee estimation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		estimator := mempool.NewEstimator(logger, client, rateCache, mempoolCache)
		return estimator.Run()
	},
}

func init() {
	RootCmd.AddCommand(mempoolCommand)
}
