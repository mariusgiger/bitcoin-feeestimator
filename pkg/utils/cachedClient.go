package utils

import (
	"encoding/base64"
	"errors"
	"log"
	"math/big"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/ybbus/jsonrpc"
	"go.uber.org/zap"
)

var (
	DefaultExpiration = 5 * time.Hour
	ErrBlockNotFound  = errors.New("block was not found")
)

type cacheItem struct {
	tx         *btcjson.TxRawResult
	expiration int64
}

type CachedRPCClient struct {
	rpcClient  *rpcclient.Client
	jsonClient jsonrpc.RPCClient
	rawTxCache map[string]*cacheItem
	janitor    *janitor
	logger     *zap.Logger

	numberToHash map[int64]string //used to allow both loading by number and hash to be cached
	//TODO numberToHash should also be cleaned up

	mu sync.RWMutex
}

// newBitcoinClient created new Bitcoin JSON RPC client
func newBitcoinClient(httpClient *http.Client, targetURL string, username, password string) (jsonrpc.RPCClient, error) {
	targetURL = "http://" + targetURL //hack
	headers := make(map[string]string)
	// then check username and password overriddes
	if username != "" || password != "" {
		headers["Authorization"] = "Basic " + basicAuth(username, password)
	}

	rpcOpts := jsonrpc.RPCClientOpts{
		CustomHeaders: headers,
		HTTPClient:    httpClient,
	}

	return jsonrpc.NewClientWithOpts(targetURL, &rpcOpts), nil // OK
}

// basicAuth converts username and password to base64-encoded string
// that can be used in `Authorization` header with `Basic` prefix
// see https://golang.org/pkg/net/http/#Request.SetBasicAuth
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func NewCachedRPCClient(btcRPCURL string, btcRPCUser string, btcRPCPassword string, logger *zap.Logger) *CachedRPCClient {
	// Connect to bitcoin core RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host:         btcRPCURL,
		User:         btcRPCUser,
		Pass:         btcRPCPassword,
		HTTPPostMode: true, // Bitcoin core only supports HTTP POST mode
		DisableTLS:   true, // Bitcoin core does not provide TLS by default
	}

	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		log.Fatal(err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{},
	}

	jsonClient, err := newBitcoinClient(httpClient, btcRPCURL, btcRPCUser, btcRPCPassword)
	if err != nil {
		log.Fatal(err)
	}

	C := &CachedRPCClient{
		rpcClient:    client,
		jsonClient:   jsonClient,
		rawTxCache:   make(map[string]*cacheItem),
		mu:           sync.RWMutex{},
		logger:       logger,
		numberToHash: make(map[int64]string),
	}

	runJanitor(C, time.Minute*5)
	runtime.SetFinalizer(C, stopJanitor)

	return C
}

func (c *CachedRPCClient) GetRawTransactionVerbose(hash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	tx, found := c.get(hash.String())
	if !found {
		rawTx, err := c.rpcClient.GetRawTransactionVerbose(hash)
		if err != nil {
			return nil, err
		}

		c.set(rawTx)
		return rawTx, nil
	}

	return tx, nil
}

func (c *CachedRPCClient) GetBlockChainInfo() (*btcjson.GetBlockChainInfoResult, error) {
	return c.rpcClient.GetBlockChainInfo()
}

func (c *CachedRPCClient) EstimateSmartFee(numBlocks int64) (float64, error) {
	type smartFeeResponse struct {
		FeeRate float64  `json:"feerate"`
		Blocks  *big.Int `json:"blocks"`
	}

	// https://bitcoincore.org/en/doc/0.17.0/rpc/util/estimatesmartfee/
	var fee smartFeeResponse
	err := c.jsonClient.CallFor(&fee, "estimatesmartfee", numBlocks)

	return fee.FeeRate, err
}

func (c *CachedRPCClient) EstimateFee(numBlocks int64) (float64, error) {
	return c.rpcClient.EstimateFee(numBlocks)
}

func (c *CachedRPCClient) GetBestBlock() (*chainhash.Hash, int32, error) {
	return c.rpcClient.GetBestBlock()
}

func (c *CachedRPCClient) GetBlockHash(height int64) (*chainhash.Hash, error) {
	return c.rpcClient.GetBlockHash(height)
}

func (c *CachedRPCClient) GetBlock(hash *chainhash.Hash) (*wire.MsgBlock, error) {
	return c.rpcClient.GetBlock(hash)
}

func (c *CachedRPCClient) GetRawMempoolVerbose() (map[string]btcjson.GetRawMempoolVerboseResult, error) {
	return c.rpcClient.GetRawMempoolVerbose()
}

func (c *CachedRPCClient) get(hash string) (*btcjson.TxRawResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, found := c.rawTxCache[hash]
	if found && item != nil {
		return item.tx, found
	}
	return nil, false
}

func (c *CachedRPCClient) set(tx *btcjson.TxRawResult) {
	c.mu.Lock()
	expiration := time.Now().Add(DefaultExpiration).UnixNano()
	c.rawTxCache[tx.Hash] = &cacheItem{tx: tx, expiration: expiration}
	c.mu.Unlock()
}

// deleteExpired all expired items from the cache.
func (c *CachedRPCClient) deleteExpired() {
	c.logger.Info("deleting expired items")
	now := time.Now().UnixNano()
	c.mu.Lock()
	for k, v := range c.rawTxCache {
		// "Inlining" of expired
		if v.expiration > 0 && now > v.expiration {
			c.logger.Info("deleted item", zap.String("key", k))
			delete(c.rawTxCache, k)
		}
	}
	c.mu.Unlock()
}

func (c *CachedRPCClient) Close() {
	c.rpcClient.WaitForShutdown()
}

type janitor struct {
	Interval time.Duration
	stop     chan bool
}

func (j *janitor) Run(c *CachedRPCClient) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.deleteExpired()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func stopJanitor(c *CachedRPCClient) {
	c.janitor.stop <- true
}

func runJanitor(c *CachedRPCClient, ci time.Duration) {
	j := &janitor{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.janitor = j
	go j.Run(c)
}
