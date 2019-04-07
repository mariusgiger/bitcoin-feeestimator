package btcutil

import (
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/mempool"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"go.uber.org/zap"
)

// Used for Fee estimation: Amount of Blocks remaining till confirmation
const (
	// BlockCountEconomical
	BlockCountEconomical = 10
	// BlockCountStandard
	BlockCountStandard = 6
	// BlockCountFast
	BlockCountFast = 2
)

type Estimator struct {
	logger         *zap.Logger
	client         *utils.CachedRPCClient
	lastSeenHeight int32
	mutex          *sync.Mutex
	feeEstimator   *FeeEstimator

	mempoolCache *feerate.MempoolCache
	scores       *scores
	ratesCache   *feerate.RateCache
}

func NewEstimator(logger *zap.Logger, client *utils.CachedRPCClient, ratesCache *feerate.RateCache, mempoolCache *feerate.MempoolCache) *Estimator {
	feeEstimator := NewFeeEstimator(
		mempool.DefaultEstimateFeeMaxRollback,
		mempool.DefaultEstimateFeeMinRegisteredBlocks)

	return &Estimator{
		feeEstimator: feeEstimator,
		client:       client,
		logger:       logger,
		mempoolCache: mempoolCache,
		ratesCache:   ratesCache,
		scores:       newScores(logger),
	}
}

// Run starts the main event loop for estimating fees
func (e *Estimator) Run() error {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	errorChannel := make(chan error)
	go func() {
		err := e.doWork()
		if err != nil {
			errorChannel <- err
		}
		for {
			select {
			case <-ticker.C:
				err := e.doWork()
				if err != nil {
					errorChannel <- err
				}
			}
		}
	}()
	return <-errorChannel
}

// These are the multipliers for bitcoin denominations.
// Example: To get the satoshi value of an amount in 'btc', use
//
//    new(big.Int).Mul(value, big.NewInt(params.BTC))
//
const (
	Satoshi = 1
	BTC     = 1e8
)

type TxDesc struct {
	// StartingPriority is the priority of the transaction when it was added
	// to the pool.
	StartingPriority float64

	// Height is the block height when the entry was added to the the source
	// pool.
	Height int64

	// Fee is the total fee the transaction associated with the entry pays.
	Fee int64

	// FeePerKB is the fee the transaction pays in Satoshi per 1000 bytes.
	FeePerKB int64

	Size int32

	Hash *chainhash.Hash
}

func (e *Estimator) doWork() error {
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

	for hash, memTx := range pool {
		err = e.registerTx(hash, memTx)
		if err != nil {
			return err
		}
	}

	if e.lastSeenHeight < info.Blocks {
		if e.lastSeenHeight != 0 && info.Blocks != e.lastSeenHeight+1 {
			//TODO treat orphaned blocks
			estimatorHeight := e.lastSeenHeight
			diff := info.Blocks - estimatorHeight
			if diff < 10 {
				e.logger.Info("getting missed blocks", zap.Any("diff", diff))
				for i := info.Blocks - diff + 1; i < info.Blocks; i++ {
					hash, err := e.client.GetBlockHash(int64(i))
					if err != nil {
						return err
					}

					block, err := e.getBlockByHash(hash)
					if err != nil {
						return err
					}

					b := btcutil.NewBlock(block)
					b.SetHeight(i)
					err = e.feeEstimator.RegisterBlock(b)
					if err != nil {
						e.logger.Error("block could not be registered", zap.String("error", err.Error()))
						return nil
					}
				}
			} else {
				e.logger.Error("too many blocks missed", zap.Any("last seen", e.lastSeenHeight), zap.Any("current", info.Blocks))
			}
		}

		//process block if not yet recorded
		hash := new(chainhash.Hash)
		err = chainhash.Decode(hash, info.BestBlockHash)
		if err != nil {
			return err
		}

		block, err := e.getBlockByHash(hash)
		if err != nil {
			return err
		}

		b := btcutil.NewBlock(block)
		b.SetHeight(info.Blocks)
		err = e.feeEstimator.RegisterBlock(b)
		if err != nil {
			e.logger.Error("block could not be registered", zap.String("error", err.Error()))
			return nil
		}

		e.lastSeenHeight = info.Blocks
	}

	economicalFeeRate, err := e.feeEstimator.EstimateFee(BlockCountEconomical)
	if err != nil {
		e.logger.Error("economical fee could not be estimated", zap.String("error", err.Error()))
		return nil
	}

	standardFeeRate, err := e.feeEstimator.EstimateFee(BlockCountStandard)
	if err != nil {
		e.logger.Error("standard fee could not be estimated", zap.String("error", err.Error()))
		return nil
	}

	fastFeeRate, err := e.feeEstimator.EstimateFee(BlockCountFast)
	if err != nil {
		e.logger.Error("fast fee could not be estimated", zap.String("error", err.Error()))
	} else {
		e.logger.Info("estimated fee", zap.Any("economical satoshi per byte", (economicalFeeRate*BTC)/1000), zap.Any("standard satoshi per byte", (standardFeeRate*BTC)/1000), zap.Any("fast satoshi per byte", (fastFeeRate*BTC)/1000))

		feeRates, err := e.ratesCache.GetFeeRatesForBlock(info.Blocks)
		if err != nil {
			return err
		}

		e.scores.addPrediction(int(info.Blocks), feeRates, float64((economicalFeeRate*BTC)/1000), float64((standardFeeRate*BTC)/1000), float64((fastFeeRate*BTC)/1000))
		return e.scores.predictScores()
	}

	return nil
}

func (e *Estimator) registerTx(hash string, memTx btcjson.GetRawMempoolVerboseResult) error {
	feeInSatoshi := int64(memTx.Fee * BTC)
	rate := (feeInSatoshi / int64(memTx.Size))
	txHash := new(chainhash.Hash)
	//e.logger.Info("registering tx", zap.Any("fee", feeInSatoshi), zap.Any("rate", rate))
	err := chainhash.Decode(txHash, hash)
	if err != nil {
		return err
	}

	txDesc := &TxDesc{
		Height:           memTx.Height,
		Fee:              feeInSatoshi,
		FeePerKB:         rate,
		Hash:             txHash,
		StartingPriority: memTx.StartingPriority,
		Size:             memTx.Size,
	}
	e.feeEstimator.ObserveTransaction(txDesc)
	return nil
}

func (e *Estimator) registerBlock() error {
	return nil
}

func (e *Estimator) getLatestBlockInfo() (*chainhash.Hash, int32, error) {
	hash, height, err := e.client.GetBestBlock()
	if err != nil {
		return nil, 0, err
	}

	return hash, height, err
}

func (e *Estimator) getBlockByHash(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	block, err := e.client.GetBlock(hash)
	if err != nil {
		return nil, err
	}

	return block, err
}
