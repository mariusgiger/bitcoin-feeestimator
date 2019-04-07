package feerate

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"
	"go.uber.org/zap"
)

// MempoolCache caches the mempool for a given block height
type MempoolCache struct {
	client             *utils.CachedRPCClient
	mempoolCache       map[int32]map[string]btcjson.GetRawMempoolVerboseResult
	logger             *zap.Logger
	lastRecordedHeight int32

	mu sync.Mutex
}

func NewMempoolCache(logger *zap.Logger, client *utils.CachedRPCClient) *MempoolCache {
	return &MempoolCache{
		client:       client,
		logger:       logger,
		mempoolCache: make(map[int32]map[string]btcjson.GetRawMempoolVerboseResult),
		mu:           sync.Mutex{},
	}
}

var (
	ErrCacheNotExists = errors.New("cache does not exist")
)

func (c *MempoolCache) GetCacheAt(height int32) (map[string]btcjson.GetRawMempoolVerboseResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if height > c.lastRecordedHeight {
		return nil, ErrCacheNotExists
	}

	cachedPool, ok := c.mempoolCache[height]
	if !ok {
		return nil, ErrCacheNotExists
	}

	c.logger.Info("using cached mempool", zap.Any("unconfirmed txs", len(cachedPool)), zap.Any("height", height))
	return cachedPool, nil
}

func (c *MempoolCache) Run() error {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	errorChannel := make(chan error)
	go func() {
		err := c.run()
		if err != nil {
			errorChannel <- err
		}
		for {
			select {
			case <-ticker.C:
				err := c.run()
				if err != nil {
					errorChannel <- err
				}
			}
		}
	}()

	return <-errorChannel
}

func (c *MempoolCache) run() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	//TODO use websockets in the future https://github.com/btcsuite/btcd/blob/master/rpcclient/examples/btcdwebsockets/main.go
	info, err := c.client.GetBlockChainInfo()
	if err != nil {
		c.logger.Error("could not get blockchain info", zap.Error(err), zap.Any("height", info.Blocks))
		return err
	}

	pool, err := c.client.GetRawMempoolVerbose()
	if err != nil {
		c.logger.Error("could not get raw mempool", zap.Error(err), zap.Any("height", info.Blocks))
		return err
	}
	c.logger.Info("updating mempool cache", zap.Any("unconfirmed txs", len(pool)), zap.Any("height", info.Blocks))
	c.lastRecordedHeight = info.Blocks
	_, ok := c.mempoolCache[info.Blocks]
	if !ok { //new block
		c.mempoolCache[info.Blocks] = pool
	} else { //only add new txs
		for hash, memTx := range pool {
			_, ok := c.mempoolCache[info.Blocks][hash]
			if !ok {
				c.mempoolCache[info.Blocks][hash] = memTx
			}
		}
	}

	return c.flush(info.Blocks)
}

func (c *MempoolCache) flush(bestHeight int32) error {
	fileName := fmt.Sprintf("mempoolcache%v.csv", bestHeight)
	f, err := os.OpenFile("./output/mempool/"+fileName, os.O_CREATE|os.O_RDWR, 0660)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	cols := []string{
		"block_number",
		"rates",
	}

	var records [][]string
	for height, pool := range c.mempoolCache {
		record := []string{
			strconv.Itoa(int(height)),
		}
		for _, entry := range pool {
			feeInSatoshi := int64(entry.Fee * utils.BTC)
			ratePerByte := (float64(feeInSatoshi) / float64(entry.Size))
			record = append(record, strconv.FormatFloat(ratePerByte, 'f', 3, 64))
		}

		records = append(records, record)
	}

	err = w.Write(cols)
	if err != nil {
		return err
	}

	return w.WriteAll(records)
}
