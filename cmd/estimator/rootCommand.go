package cmd

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger       *zap.Logger
	rateCache    *feerate.RateCache
	client       *utils.CachedRPCClient
	mempoolCache *feerate.MempoolCache
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "estimator",
	Short: "btcfeeestimator",
	Long:  `Bitcoin fee estimator.`,
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		client.Close()
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		log.Fatalf("Something went terribly wrong: %v", err)
		os.Exit(-1)
	}
}

var (
	options struct {
		btcRPCURL      string
		btcRPCUser     string
		btcRPCPassword string
	}
)

func init() {
	logger, _ = zap.NewDevelopment(zap.AddStacktrace(zapcore.FatalLevel))

	naiveCommand.Flags().StringVarP(&options.btcRPCURL, "url", "", "13.80.132.186:8332", "bitcoin rpc url")
	naiveCommand.Flags().StringVarP(&options.btcRPCUser, "user", "u", "bitcoinrpc", "bitcoin rpc username")
	naiveCommand.Flags().StringVarP(&options.btcRPCPassword, "password", "p", "eaf672111c88b64fc436f01259dd1812", "bitcoin rpc password")

	client = utils.NewCachedRPCClient(options.btcRPCURL, options.btcRPCUser, options.btcRPCPassword, logger)
	rateCache = feerate.NewRateCache(client, logger)
	mempoolCache = feerate.NewMempoolCache(logger, client)

	go func() {
		err := mempoolCache.Run()
		if err != nil {
			logger.Fatal("mempool cache error", zap.Error(err))
		}
	}()
}
