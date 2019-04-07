package naive

import (
	"sort"
	"time"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"go.uber.org/zap"
)

// Estimator implementation of a naive bitcoin fee estimator
type Estimator struct {
	client             *utils.CachedRPCClient
	logger             *zap.Logger
	lastObservedHeight int32
	scores             *scores
	ratesCache         *feerate.RateCache
}

// NewEstimator creates a new naive bitcoin fee estimator
func NewEstimator(logger *zap.Logger, client *utils.CachedRPCClient, ratesCache *feerate.RateCache) *Estimator {
	return &Estimator{
		client:     client,
		logger:     logger,
		scores:     newScores(logger),
		ratesCache: ratesCache,
	}
}

// Run starts the main event loop for estimating fees
func (e *Estimator) Run() error {
	ticker := time.NewTicker(time.Minute * 1)
	defer ticker.Stop()

	errorChannel := make(chan error)
	go func() {
		err := e.EstimateFee()
		if err != nil {
			errorChannel <- err
		}
		for {
			select {
			case <-ticker.C:
				err := e.EstimateFee()
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
func (e *Estimator) EstimateFee() error {
	info, err := e.client.GetBlockChainInfo()
	if err != nil {
		return err
	}

	if info.Blocks <= e.lastObservedHeight {
		e.logger.Info("already estimated")
		return nil
	}

	e.logger.Info("got info", zap.Any("info", info))
	hash := new(chainhash.Hash)
	err = chainhash.Decode(hash, info.BestBlockHash)
	if err != nil {
		return err
	}

	feeRates, err := e.ratesCache.GetFeeRatesForBlock(info.Blocks)
	if err != nil {
		return err
	}

	e.lastObservedHeight = info.Blocks
	rate := SuggestFeeRate(feeRates.Rates)
	e.scores.addPrediction(int(info.Blocks), feeRates, rate)
	e.scores.predictScores()
	return nil
}

var (
	//Percentile defines the position where the fee rate is estimated
	//e.g. 50 means median value, 60 means a fee that is a little bit higher than the median
	Percentile = 60
)

// SuggestFeeRate returns the recommended fee rate in Satoshi per byte
func SuggestFeeRate(feeRates []int) int {
	if len(feeRates) > 0 {
		sort.Ints(feeRates)
		rate := feeRates[(len(feeRates)-1)*Percentile/100]

		if rate > utils.MaxFeeRate {
			rate = utils.MaxFeeRate
		}
		return rate
	}

	return 0
}

func (e *Estimator) getLatestBlockInfo() (*chainhash.Hash, int32, error) {
	hash, height, err := e.client.GetBestBlock()
	if err != nil {
		return nil, 0, err
	}

	return hash, height, err
}
