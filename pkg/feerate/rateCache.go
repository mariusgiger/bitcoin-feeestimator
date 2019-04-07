package feerate

import (
	"errors"
	"sync"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"
	"go.uber.org/zap"
)

// RateCache caches fee rates for a given block height
type RateCache struct {
	rpcClient *utils.CachedRPCClient
	cache     map[int32]*FeeRates
	logger    *zap.Logger

	heightMutex *utils.Mutex
	mu          sync.RWMutex
}

type FeeRates struct {
	Rates       []int
	NumberOfTxs int
}

// NewRateCache returns a new fee rate cache
func NewRateCache(rpcClient *utils.CachedRPCClient, logger *zap.Logger) *RateCache {
	maxRetry := 200
	maxDelay := float64(1000000000000) // 1000 second
	baseDelay := float64(1000000)      // 1000000 nanosecond
	factor := float64(1.3)
	jitter := float64(0.2)

	return &RateCache{
		rpcClient:   rpcClient,
		cache:       make(map[int32]*FeeRates),
		logger:      logger,
		heightMutex: utils.NewCustomizedMapMutex(maxRetry, maxDelay, baseDelay, factor, jitter),
		mu:          sync.RWMutex{},
	}
}

// GetFeeRatesForBlock returns fee rates for given block in Sathoshi per Byte
func (c *RateCache) GetFeeRatesForBlock(height int32) (*FeeRates, error) {
	c.logger.Info("getting rates for block", zap.Int32("block", height))
	c.mu.RLock()

	rates, ok := c.cache[height]
	c.mu.RUnlock()
	if ok {
		c.logger.Info("already cached rates", zap.Int32("block", height))
		return rates, nil
	}

	gotLock := c.heightMutex.TryLock(height)
	if !gotLock {
		return nil, errors.New("could not lock fee rates for height")
	}
	defer c.heightMutex.Unlock(height)

	rates, err := c.getFeeRates(height)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[height] = rates
	c.mu.Unlock()

	c.logger.Info("got rates", zap.Any("rates", rates))
	return rates, nil
}

func (c *RateCache) getFeeRates(height int32) (*FeeRates, error) {
	hash, err := c.rpcClient.GetBlockHash(int64(height))
	if err != nil {
		return nil, err
	}

	block, err := c.rpcClient.GetBlock(hash)
	if err != nil {
		return nil, err
	}

	type processTxResult struct {
		rate int
		err  error
	}

	feeRates := make([]int, 0)
	ch := make(chan processTxResult, len(block.Transactions))
	exp := 0
	for i := 0; i < len(block.Transactions); i++ {
		tx := block.Transactions[i]
		go func() {
			rate, err := c.processTx(tx)
			if err != nil {
				ch <- processTxResult{0, err}
			} else {
				if rate > 0 {
					ch <- processTxResult{rate, nil}
				} else {
					ch <- processTxResult{0, nil}
				}
			}
		}()

		exp++
	}

	for exp > 0 {
		res := <-ch
		if res.err != nil {
			c.logger.Error("an error occurred", zap.Error(res.err))
		}
		exp--
		if res.rate != 0 {
			feeRates = append(feeRates, res.rate)
			continue
		}
		//TODO handle failed --> possibly reload or ignore as it is in gasPriceOracle
	}

	return &FeeRates{Rates: feeRates, NumberOfTxs: len(block.Transactions)}, nil
}

func (c *RateCache) processTx(tx *wire.MsgTx) (int, error) {
	hash := tx.TxHash()
	rawTx, err := c.rpcClient.GetRawTransactionVerbose(&hash)
	if err != nil {
		c.logger.Error("could not get tx", zap.Any("hash", hash), zap.String("error", err.Error()))
		return 0, err
	}

	inputSum := float64(0)
	for _, input := range rawTx.Vin {
		if input.IsCoinBase() {
			return 0, nil
		}

		if input.HasWitness() {
			//e.logger.Info("skipped segwit")
			return 0, nil //TODO handle
		}

		inputHash := new(chainhash.Hash)
		err = chainhash.Decode(inputHash, input.Txid)
		if err != nil {
			return 0, err
		}

		inputTx, err := c.rpcClient.GetRawTransactionVerbose(inputHash)
		if err != nil {
			return 0, err
		}

		if len(inputTx.Vout) <= int(input.Vout) {
			return 0, errors.New("too little outputs in inputTx")
		}

		inputSum += inputTx.Vout[input.Vout].Value
	}

	outputSum := float64(0)
	for _, output := range rawTx.Vout {
		outputSum += output.Value
	}

	fee := inputSum - outputSum
	feeInSatoshi := fee * utils.BTC //NOTE this can be really high, users constantly overpay the miners e.g. x20 compared to estimatesmartfee of BTC
	size := tx.SerializeSize()      //TODO should this be SerializeSizeStripped in case of segwit?
	rate := feeInSatoshi / float64(size)
	return int(rate), nil
}
