package mempool

import (
	"sort"
	"time"

	"github.com/btcsuite/btcd/btcjson"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"
	"go.uber.org/zap"
)

// Estimator implementation of a bitcoin fee estimator based on the current mempool
type Estimator struct {
	client             *utils.CachedRPCClient
	logger             *zap.Logger
	lastObservedHeight int32
	scores             *scores
	ratesCache         *feerate.RateCache
	mempoolCache       *feerate.MempoolCache
}

// NewEstimator creates a new naive bitcoin fee estimator
func NewEstimator(logger *zap.Logger, client *utils.CachedRPCClient, ratesCache *feerate.RateCache, mempoolCache *feerate.MempoolCache) *Estimator {
	return &Estimator{
		client:       client,
		logger:       logger,
		scores:       newScores(logger),
		ratesCache:   ratesCache,
		mempoolCache: mempoolCache,
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

//EstimateFee runs the estimation
func (e *Estimator) EstimateFee() error {
	info, err := e.client.GetBlockChainInfo()
	if err != nil {
		return err
	}

	pool, err := e.mempoolCache.GetCacheAt(info.Blocks)
	if err != nil {
		if err == feerate.ErrCacheNotExists {
			e.logger.Info("mem cache does not exist", zap.Any("height", info.Blocks))
			return nil
		}

		return err
	}

	avgBlockSize, lastMined, err := e.getAverageBlockSize(int(info.Blocks))
	if err != nil {
		return err
	}

	diff := time.Now().Sub(lastMined)
	powProgress := (float64(1) / float64(10)) * float64(diff.Minutes())
	e.logger.Info("time", zap.Any("pow", powProgress), zap.Any("now", time.Now()), zap.Any("lastMined", lastMined), zap.Any("diff", diff))
	if powProgress > 1 {
		powProgress = 1
	}

	poolRates := e.getPoolRates(pool)
	sort.Float64s(poolRates)

	idx := len(poolRates) - avgBlockSize
	if idx < 0 {
		idx = 0
	}

	blockWindowRates := poolRates[idx:]
	verificationPercentile := float64(Percentile) - float64(Range)*powProgress
	estimate := blockWindowRates[(len(blockWindowRates)-1)*int(verificationPercentile)/100]
	e.logger.Info("estimated mempool rate", zap.Any("rate", estimate), zap.Any("percentile", verificationPercentile), zap.Any("txs", len(blockWindowRates)))

	feeRates, err := e.ratesCache.GetFeeRatesForBlock(info.Blocks)
	if err != nil {
		return err
	}

	e.scores.addPrediction(int(info.Blocks), feeRates, estimate)
	e.scores.predictScores()
	return nil
}

func (e *Estimator) getAverageBlockSize(height int) (int, time.Time, error) {
	numberOfBlocks := 5
	numberOfTxs := 0
	var time time.Time
	for i := height; i > height-numberOfBlocks; i-- {
		hash, err := e.client.GetBlockHash(int64(i))
		if err != nil {
			return 0, time, err
		}

		block, err := e.client.GetBlock(hash)
		if err != nil {
			return 0, time, err
		}

		if i == height {
			time = block.Header.Timestamp
		}
		numberOfTxs = numberOfTxs + len(block.Transactions)
	}

	return numberOfTxs / numberOfBlocks, time, nil
}

func (e *Estimator) getPoolRates(pool map[string]btcjson.GetRawMempoolVerboseResult) []float64 {
	var rates []float64
	for _, entry := range pool {
		feeInSatoshi := int64(entry.Fee * utils.BTC)
		ratePerByte := (float64(feeInSatoshi) / float64(entry.Size))
		rates = append(rates, ratePerByte)
	}

	return rates
}

var (
	//Percentile defines the position where the fee rate is estimated
	//e.g. 50 means median value, 60 means a fee that is a little bit higher than the median
	Percentile = 80
	Range      = 60
)

func (e *Estimator) estimateFee() (float64, error) {
	info, err := e.client.GetBlockChainInfo()
	if err != nil {
		return 0, err
	}

	pool, err := e.mempoolCache.GetCacheAt(info.Blocks)
	if err != nil {
		return 0, err
	}

	avgBlockSize, lastMined, err := e.getAverageBlockSize(int(info.Blocks))
	if err != nil {
		return 0, err
	}

	diff := time.Now().Sub(lastMined)
	powProgress := (float64(1) / float64(10)) * float64(diff.Minutes())
	if powProgress > 1 {
		powProgress = 1
	}

	poolRates := e.getPoolRates(pool)
	sort.Float64s(poolRates)

	idx := len(poolRates) - avgBlockSize
	if idx < 0 {
		idx = 0
	}

	blockWindowRates := poolRates[idx:]
	verificationPercentile := float64(Percentile) - float64(Range)*powProgress
	estimate := blockWindowRates[(len(blockWindowRates)-1)*int(verificationPercentile)/100]
	return estimate, nil
}
