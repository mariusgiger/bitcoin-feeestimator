package cmd

import (
	"github.com/spf13/cobra"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/simulation"
)

// btcutilCommand represents the command for btcuitl estimation
var simCommand = &cobra.Command{
	Use:   "sim",
	Short: "Runs fee estimation simulation",
	Long:  `Runs fee estimation simulation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sim := simulation.NewSimulation(logger)
		return sim.Run()
	},
}

func init() {
	RootCmd.AddCommand(simCommand)
}
