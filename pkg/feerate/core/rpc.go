package core

import (
	"time"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"

	"go.uber.org/zap"
)

// RPCEstimator implementation of a core bitcoin fee estimator using json rpc
type RPCEstimator struct {
	client             *utils.CachedRPCClient
	logger             *zap.Logger
	lastObservedHeight int32
	scores             *scores
	ratesCache         *feerate.RateCache
}

// NewRPCEstimator creates a new core bitcoin fee estimator based on rpc calls
func NewRPCEstimator(logger *zap.Logger, client *utils.CachedRPCClient, ratesCache *feerate.RateCache) *RPCEstimator {
	return &RPCEstimator{
		client:     client,
		logger:     logger,
		scores:     newScores(logger),
		ratesCache: ratesCache,
	}
}

// Run starts the main event loop for estimating fees
func (e *RPCEstimator) Run() error {
	ticker := time.NewTicker(time.Minute * 1)
	defer ticker.Stop()

	errorChannel := make(chan error)
	go func() {
		// err := e.estimateFee()
		// if err != nil {
		// 	errorChannel <- err
		// }

		err := e.estimateSmartFee()
		if err != nil {
			errorChannel <- err
		}
		for {
			select {
			case <-ticker.C:
				// err := e.estimateFee()
				// if err != nil {
				// 	errorChannel <- err
				// }

				err = e.estimateSmartFee()
				if err != nil {
					errorChannel <- err
				}
			}
		}
	}()

	return <-errorChannel
}

type txCacheEntry struct {
	fees int
}

//EstimateFee runs the estimation
func (e *RPCEstimator) estimateFee() error {
	info, err := e.client.GetBlockChainInfo()
	if err != nil {
		return err
	}

	if info.Blocks <= e.lastObservedHeight {
		e.logger.Info("already estimated")
		return nil
	}

	e.lastObservedHeight = info.Blocks
	rate, err := e.client.EstimateFee(BlockCountStandard)
	if err != nil {
		return err
	}
	e.logger.Info("got rate", zap.Any("rate", rate))

	// feeRates, err := e.ratesCache.GetFeeRatesForBlock(info.Blocks)
	// if err != nil {
	// 	return err
	// }

	// e.scores.addPrediction(int(info.Blocks), feeRates, int(rate))
	return nil
}

// Used for Fee estimation: Amount of Blocks remaining till confirmation
const (
	// BlockCountEconomical
	BlockCountEconomical = 10
	// BlockCountStandard
	BlockCountStandard = 6
	// BlockCountFast
	BlockCountFast = 2
)

func (e *RPCEstimator) estimateSmartFee() error {
	info, err := e.client.GetBlockChainInfo()
	if err != nil {
		return err
	}

	economical, err := e.client.EstimateSmartFee(BlockCountEconomical)
	if err != nil {
		return err
	}
	economical = economical / 1000 * utils.BTC

	standard, err := e.client.EstimateSmartFee(BlockCountStandard)
	if err != nil {
		return err
	}
	standard = standard / 1000 * utils.BTC

	fast, err := e.client.EstimateSmartFee(BlockCountFast)
	if err != nil {
		return err
	}
	fast = fast / 1000 * utils.BTC
	e.logger.Info("got smart rates", zap.Any("economical", economical), zap.Any("standard", standard), zap.Any("fast", fast))

	if e.lastObservedHeight < info.Blocks {
		feeRates, err := e.ratesCache.GetFeeRatesForBlock(info.Blocks)
		if err != nil {
			return err
		}

		e.lastObservedHeight = info.Blocks
		e.scores.addPrediction(int(info.Blocks), feeRates, economical, standard, fast)
		return e.scores.predictScores()
	}

	return nil
}
