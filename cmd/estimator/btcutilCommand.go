package cmd

import (
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate/btcutil"
	"github.com/spf13/cobra"
)

// btcutilCommand represents the command for btcuitl estimation
var btcutilCommand = &cobra.Command{
	Use:   "btcutil",
	Short: "Runs the btcutil fee estimation",
	Long:  `Runs the btcutil fee estimation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		estimator := btcutil.NewEstimator(logger, client, rateCache, mempoolCache)
		return estimator.Run()
	},
}

func init() {
	RootCmd.AddCommand(btcutilCommand)
}
