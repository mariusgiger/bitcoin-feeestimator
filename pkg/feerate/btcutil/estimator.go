package btcutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/mining"
	"github.com/btcsuite/btcutil"
	"github.com/ethereum/go-ethereum/log"
)

// TODO incorporate Alex Morcos' modifications to Gavin's initial model
// https://lists.linuxfoundation.org/pipermail/bitcoin-dev/2014-October/006824.html

const (
	// estimateFeeDepth is the maximum number of blocks before a transaction
	// is confirmed that we want to track.
	estimateFeeDepth = 25

	// estimateFeeBinSize is the number of txs stored in each bin.
	estimateFeeBinSize = 100

	// estimateFeeMaxReplacements is the max number of replacements that
	// can be made by the txs found in a given block.
	estimateFeeMaxReplacements = 10

	// DefaultEstimateFeeMaxRollback is the default number of rollbacks
	// allowed by the fee estimator for orphaned blocks.
	DefaultEstimateFeeMaxRollback = 2

	// DefaultEstimateFeeMinRegisteredBlocks is the default minimum
	// number of blocks which must be observed by the fee estimator before
	// it will provide fee estimations.
	DefaultEstimateFeeMinRegisteredBlocks = 3

	bytePerKb = 1000

	btcPerSatoshi = 1E-8
)

var (
	// EstimateFeeDatabaseKey is the key that we use to
	// store the fee estimator in the database.
	EstimateFeeDatabaseKey = []byte("estimatefee")
)

// SatoshiPerByte is number with units of satoshis per byte.
type SatoshiPerByte float64

// BtcPerKilobyte is number with units of bitcoins per kilobyte.
type BtcPerKilobyte float64

// ToBtcPerKb returns a float value that represents the given
// SatoshiPerByte converted to satoshis per kb.
func (rate SatoshiPerByte) ToBtcPerKb() BtcPerKilobyte {
	// If our rate is the error value, return that.
	if rate == SatoshiPerByte(-1.0) {
		return -1.0
	}

	return BtcPerKilobyte(float64(rate) * bytePerKb * btcPerSatoshi)
}

// Fee returns the fee for a transaction of a given size for
// the given fee rate.
func (rate SatoshiPerByte) Fee(size uint32) btcutil.Amount {
	// If our rate is the error value, return that.
	if rate == SatoshiPerByte(-1) {
		return btcutil.Amount(-1)
	}

	return btcutil.Amount(float64(rate) * float64(size))
}

// NewSatoshiPerByte creates a SatoshiPerByte from an Amount and a
// size in bytes.
func NewSatoshiPerByte(fee btcutil.Amount, size uint32) SatoshiPerByte {
	return SatoshiPerByte(float64(fee) / float64(size))
}

// observedTransaction represents an observed transaction and some
// additional data required for the fee estimation algorithm.
type observedTransaction struct {
	// A transaction hash.
	hash chainhash.Hash

	// The fee per byte of the transaction in satoshis.
	feeRate SatoshiPerByte

	// The block height when it was observed.
	observed int32

	// The height of the block in which it was mined.
	// If the transaction has not yet been mined, it is zero.
	mined int32
}

// registeredBlock has the hash of a block and the list of transactions
// it mined which had been previously observed by the FeeEstimator. It
// is used if Rollback is called to reverse the effect of registering
// a block.
type registeredBlock struct {
	hash         chainhash.Hash
	transactions []*observedTransaction
}

// FeeEstimator manages the data necessary to create
// fee estimations. It is safe for concurrent access.
type FeeEstimator struct {
	maxRollback uint32
	binSize     int32

	// The maximum number of replacements that can be made in a single
	// bin per block. Default is estimateFeeMaxReplacements
	maxReplacements int32

	// The minimum number of blocks that can be registered with the fee
	// estimator before it will provide answers.
	minRegisteredBlocks uint32

	// The last known height.
	LastKnownHeight int32

	// The number of blocks that have been registered.
	numBlocksRegistered uint32

	mtx      sync.RWMutex
	observed map[chainhash.Hash]*observedTransaction
	bin      [estimateFeeDepth][]*observedTransaction

	// The cached estimates.
	cached []SatoshiPerByte

	// Transactions that have been removed from the bins. This allows us to
	// revert in case of an orphaned block.
	dropped []*registeredBlock
}

// NewFeeEstimator creates a FeeEstimator for which at most maxRollback blocks
// can be unregistered and which returns an error unless minRegisteredBlocks
// have been registered with it.
func NewFeeEstimator(maxRollback, minRegisteredBlocks uint32) *FeeEstimator {
	return &FeeEstimator{
		maxRollback:         maxRollback,
		minRegisteredBlocks: minRegisteredBlocks,
		LastKnownHeight:     mining.UnminedHeight,
		binSize:             estimateFeeBinSize,
		maxReplacements:     estimateFeeMaxReplacements,
		observed:            make(map[chainhash.Hash]*observedTransaction),
		dropped:             make([]*registeredBlock, 0, maxRollback),
	}
}

// ObserveTransaction is called when a new transaction is observed in the mempool.
func (ef *FeeEstimator) ObserveTransaction(t *TxDesc) {
	ef.mtx.Lock()
	defer ef.mtx.Unlock()

	// If we haven't seen a block yet we don't know when this one arrived,
	// so we ignore it.
	if ef.LastKnownHeight == mining.UnminedHeight {
		return
	}

	hash := *t.Hash
	if _, ok := ef.observed[hash]; !ok {
		//size := uint32(GetTxVirtualSize(t.Tx))

		size := t.Size
		ef.observed[hash] = &observedTransaction{
			hash:     hash,
			feeRate:  NewSatoshiPerByte(btcutil.Amount(t.Fee), uint32(size)),
			observed: int32(t.Height),
			mined:    mining.UnminedHeight,
		}
	}
}

// RegisterBlock informs the fee estimator of a new block to take into account.
func (ef *FeeEstimator) RegisterBlock(block *btcutil.Block) error {
	ef.mtx.Lock()
	defer ef.mtx.Unlock()

	// The previous sorted list is invalid, so delete it.
	ef.cached = nil

	height := block.Height()
	if height != ef.LastKnownHeight+1 && ef.LastKnownHeight != mining.UnminedHeight {
		return fmt.Errorf("intermediate block not recorded; current height is %d; new height is %d",
			ef.LastKnownHeight, height)
	}

	// Update the last known height.
	ef.LastKnownHeight = height
	ef.numBlocksRegistered++

	// Randomly order txs in block.
	transactions := make(map[*btcutil.Tx]struct{})
	for _, t := range block.Transactions() {
		transactions[t] = struct{}{}
	}

	// Count the number of replacements we make per bin so that we don't
	// replace too many.
	var replacementCounts [estimateFeeDepth]int

	// Keep track of which txs were dropped in case of an orphan block.
	dropped := &registeredBlock{
		hash:         *block.Hash(),
		transactions: make([]*observedTransaction, 0, 100),
	}

	// Go through the txs in the block.
	for t := range transactions {
		hash := *t.Hash()

		// Have we observed this tx in the mempool?
		o, ok := ef.observed[hash]
		if !ok {
			continue
		}

		// Put the observed tx in the oppropriate bin.
		blocksToConfirm := height - o.observed - 1

		// This shouldn't happen if the fee estimator works correctly,
		// but return an error if it does.
		if o.mined != mining.UnminedHeight {
			log.Error("Estimate fee: transaction ", hash.String(), " has already been mined")
			return errors.New("Transaction has already been mined")
		}

		// This shouldn't happen but check just in case to avoid
		// an out-of-bounds array index later.
		if blocksToConfirm >= estimateFeeDepth {
			continue
		}

		// Make sure we do not replace too many transactions per min.
		if replacementCounts[blocksToConfirm] == int(ef.maxReplacements) {
			continue
		}

		o.mined = height

		replacementCounts[blocksToConfirm]++

		bin := ef.bin[blocksToConfirm]

		// Remove a random element and replace it with this new tx.
		if len(bin) == int(ef.binSize) {
			// Don't drop transactions we have just added from this same block.
			l := int(ef.binSize) - replacementCounts[blocksToConfirm]
			drop := rand.Intn(l)
			dropped.transactions = append(dropped.transactions, bin[drop])

			bin[drop] = bin[l-1]
			bin[l-1] = o
		} else {
			bin = append(bin, o)
		}
		ef.bin[blocksToConfirm] = bin
	}

	// Go through the mempool for txs that have been in too long.
	for hash, o := range ef.observed {
		if o.mined == mining.UnminedHeight && height-o.observed >= estimateFeeDepth {
			delete(ef.observed, hash)
		}
	}

	// Add dropped list to history.
	if ef.maxRollback == 0 {
		return nil
	}

	if uint32(len(ef.dropped)) == ef.maxRollback {
		ef.dropped = append(ef.dropped[1:], dropped)
	} else {
		ef.dropped = append(ef.dropped, dropped)
	}

	return nil
}

// GetLastKnownHeight returns the height of the last block which was registered.
func (ef *FeeEstimator) GetLastKnownHeight() int32 {
	ef.mtx.Lock()
	defer ef.mtx.Unlock()

	return ef.LastKnownHeight
}

// Rollback unregisters a recently registered block from the FeeEstimator.
// This can be used to reverse the effect of an orphaned block on the fee
// estimator. The maximum number of rollbacks allowed is given by
// maxRollbacks.
//
// Note: not everything can be rolled back because some transactions are
// deleted if they have been observed too long ago. That means the result
// of Rollback won't always be exactly the same as if the last block had not
// happened, but it should be close enough.
func (ef *FeeEstimator) Rollback(hash *chainhash.Hash) error {
	ef.mtx.Lock()
	defer ef.mtx.Unlock()

	// Find this block in the stack of recent registered blocks.
	var n int
	for n = 1; n <= len(ef.dropped); n++ {
		if ef.dropped[len(ef.dropped)-n].hash.IsEqual(hash) {
			break
		}
	}

	if n > len(ef.dropped) {
		return errors.New("no such block was recently registered")
	}

	for i := 0; i < n; i++ {
		ef.rollback()
	}

	return nil
}

// rollback rolls back the effect of the last block in the stack
// of registered blocks.
func (ef *FeeEstimator) rollback() {
	// The previous sorted list is invalid, so delete it.
	ef.cached = nil

	// pop the last list of dropped txs from the stack.
	last := len(ef.dropped) - 1
	if last == -1 {
		// Cannot really happen because the exported calling function
		// only rolls back a block already known to be in the list
		// of dropped transactions.
		return
	}

	dropped := ef.dropped[last]

	// where we are in each bin as we replace txs?
	var replacementCounters [estimateFeeDepth]int

	// Go through the txs in the dropped block.
	for _, o := range dropped.transactions {
		// Which bin was this tx in?
		blocksToConfirm := o.mined - o.observed - 1

		bin := ef.bin[blocksToConfirm]

		var counter = replacementCounters[blocksToConfirm]

		// Continue to go through that bin where we left off.
		for {
			if counter >= len(bin) {
				// Panic, as we have entered an unrecoverable invalid state.
				panic(errors.New("illegal state: cannot rollback dropped transaction"))
			}

			prev := bin[counter]

			if prev.mined == ef.LastKnownHeight {
				prev.mined = mining.UnminedHeight

				bin[counter] = o

				counter++
				break
			}

			counter++
		}

		replacementCounters[blocksToConfirm] = counter
	}

	// Continue going through bins to find other txs to remove
	// which did not replace any other when they were entered.
	for i, j := range replacementCounters {
		for {
			l := len(ef.bin[i])
			if j >= l {
				break
			}

			prev := ef.bin[i][j]

			if prev.mined == ef.LastKnownHeight {
				prev.mined = mining.UnminedHeight

				newBin := append(ef.bin[i][0:j], ef.bin[i][j+1:l]...)
				// TODO This line should prevent an unintentional memory
				// leak but it causes a panic when it is uncommented.
				// ef.bin[i][j] = nil
				ef.bin[i] = newBin

				continue
			}

			j++
		}
	}

	ef.dropped = ef.dropped[0:last]

	// The number of blocks the fee estimator has seen is decrimented.
	ef.numBlocksRegistered--
	ef.LastKnownHeight--
}

// estimateFeeSet is a set of txs that can that is sorted
// by the fee per kb rate.
type estimateFeeSet struct {
	feeRate []SatoshiPerByte
	bin     [estimateFeeDepth]uint32
}

func (b *estimateFeeSet) Len() int { return len(b.feeRate) }

func (b *estimateFeeSet) Less(i, j int) bool {
	return b.feeRate[i] > b.feeRate[j]
}

func (b *estimateFeeSet) Swap(i, j int) {
	b.feeRate[i], b.feeRate[j] = b.feeRate[j], b.feeRate[i]
}

// estimateFee returns the estimated fee for a transaction
// to confirm in confirmations blocks from now, given
// the data set we have collected.
func (b *estimateFeeSet) estimateFee(confirmations int) SatoshiPerByte {
	if confirmations <= 0 {
		return SatoshiPerByte(math.Inf(1))
	}

	if confirmations > estimateFeeDepth {
		return 0
	}

	// We don't have any transactions!
	if len(b.feeRate) == 0 {
		return 0
	}

	var min, max int = 0, 0
	for i := 0; i < confirmations-1; i++ {
		min += int(b.bin[i])
	}

	max = min + int(b.bin[confirmations-1]) - 1
	if max < min {
		max = min
	}
	feeIndex := (min + max) / 2
	if feeIndex >= len(b.feeRate) {
		feeIndex = len(b.feeRate) - 1
	}

	return b.feeRate[feeIndex]
}

// newEstimateFeeSet creates a temporary data structure that
// can be used to find all fee estimates.
func (ef *FeeEstimator) newEstimateFeeSet() *estimateFeeSet {
	set := &estimateFeeSet{}

	capacity := 0
	for i, b := range ef.bin {
		l := len(b)
		set.bin[i] = uint32(l)
		capacity += l
	}

	set.feeRate = make([]SatoshiPerByte, capacity)

	i := 0
	for _, b := range ef.bin {
		for _, o := range b {
			set.feeRate[i] = o.feeRate
			i++
		}
	}

	sort.Sort(set)

	return set
}

// estimates returns the set of all fee estimates from 1 to estimateFeeDepth
// confirmations from now.
func (ef *FeeEstimator) estimates() []SatoshiPerByte {
	set := ef.newEstimateFeeSet()

	estimates := make([]SatoshiPerByte, estimateFeeDepth)
	for i := 0; i < estimateFeeDepth; i++ {
		estimates[i] = set.estimateFee(i + 1)
	}

	return estimates
}

// EstimateFee estimates the fee per byte to have a tx confirmed a given
// number of blocks from now.
func (ef *FeeEstimator) EstimateFee(numBlocks uint32) (BtcPerKilobyte, error) {
	ef.mtx.Lock()
	defer ef.mtx.Unlock()

	// If the number of registered blocks is below the minimum, return
	// an error.
	if ef.numBlocksRegistered < ef.minRegisteredBlocks {
		return -1, errors.New("not enough blocks have been observed")
	}

	if numBlocks == 0 {
		return -1, errors.New("cannot confirm transaction in zero blocks")
	}

	if numBlocks > estimateFeeDepth {
		return -1, fmt.Errorf(
			"can only estimate fees for up to %d blocks from now",
			estimateFeeBinSize)
	}

	// If there are no cached results, generate them.
	if ef.cached == nil {
		ef.cached = ef.estimates()
	}

	return ef.cached[int(numBlocks)-1].ToBtcPerKb(), nil
}

// In case the format for the serialized version of the FeeEstimator changes,
// we use a version number. If the version number changes, it does not make
// sense to try to upgrade a previous version to a new version. Instead, just
// start fee estimation over.
const estimateFeeSaveVersion = 1

func deserializeRegisteredBlock(r io.Reader, txs map[uint32]*observedTransaction) (*registeredBlock, error) {
	var lenTransactions uint32

	rb := &registeredBlock{}
	binary.Read(r, binary.BigEndian, &rb.hash)
	binary.Read(r, binary.BigEndian, &lenTransactions)

	rb.transactions = make([]*observedTransaction, lenTransactions)

	for i := uint32(0); i < lenTransactions; i++ {
		var index uint32
		binary.Read(r, binary.BigEndian, &index)
		rb.transactions[i] = txs[index]
	}

	return rb, nil
}

// FeeEstimatorState represents a saved FeeEstimator that can be
// restored with data from an earlier session of the program.
type FeeEstimatorState []byte

// observedTxSet is a set of txs that can that is sorted
// by hash. It exists for serialization purposes so that
// a serialized state always comes out the same.
type observedTxSet []*observedTransaction

func (q observedTxSet) Len() int { return len(q) }

func (q observedTxSet) Less(i, j int) bool {
	return strings.Compare(q[i].hash.String(), q[j].hash.String()) < 0
}

func (q observedTxSet) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
}
