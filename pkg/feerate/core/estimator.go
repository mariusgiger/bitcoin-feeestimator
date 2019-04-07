package core

import "log"

type TxStatsInfo struct {
	blockHeight uint
	bucketIndex int
}

var (
	//Track confirm delays up to 12 blocks for short horizon
	ShortBlockPeriods = 12
	ShortScale        = uint(1)
	/** Track confirm delays up to 48 blocks for medium horizon */
	MedBlockPeriods = 24
	MedScale        = uint(2)
	/** Track confirm delays up to 1008 blocks for long horizon */
	LongBlockPeriods = 42
	LongScale        = uint(24)
	/** Historical estimates that are older than this aren't valid */
	OldestEstimateHistory = uint(6 * 1008)

	/** Decay of .962 is a half-life of 18 blocks or about 3 hours */
	ShortDecay = .962
	/** Decay of .998 is a half-life of 144 blocks or about 1 day */
	MedDecay = .9952
	/** Decay of .9995 is a half-life of 1008 blocks or about 1 week */
	LongDecay = .99931

	/** Require greater than 60% of X feerate transactions to be confirmed within Y/2 blocks*/
	HalfSuccessPct = .6
	/** Require greater than 85% of X feerate transactions to be confirmed within Y blocks*/
	SuccessPct = .85
	/** Require greater than 95% of X feerate transactions to be confirmed within 2 * Y blocks*/
	DoubleSuccessPct = .95

	/** Require an avg of 0.1 tx in the combined feerate bucket per block to have stat significance */
	SufficientFeeTxs = 0.1
	/** Require an avg of 0.5 tx when using short decay since there are fewer blocks considered*/
	SufficientTxsShort = 0.5

	InfFeeRate       = 1e99
	MinBucketFeeRate = float64(1000)
	MaxBucketFeeRate = 1e7

	//   Spacing of FeeRate buckets
	//   We have to lump transactions into buckets based on feerate, but we want to be able
	//   to give accurate estimates over a large range of potential feerates
	//   Therefore it makes sense to exponentially space the buckets
	FeeSpacing = 1.05
)

/** \class CBlockPolicyEstimator
 * The BlockPolicyEstimator is used for estimating the feerate needed
 * for a transaction to be included in a block within a certain number of
 * blocks.
 *
 * At a high level the algorithm works by grouping transactions into buckets
 * based on having similar feerates and then tracking how long it
 * takes transactions in the various buckets to be mined.  It operates under
 * the assumption that in general transactions of higher feerate will be
 * included in blocks before transactions of lower feerate.   So for
 * example if you wanted to know what feerate you should put on a transaction to
 * be included in a block within the next 5 blocks, you would start by looking
 * at the bucket with the highest feerate transactions and verifying that a
 * sufficiently high percentage of them were confirmed within 5 blocks and
 * then you would look at the next highest feerate bucket, and so on, stopping at
 * the last bucket to pass the test.   The average feerate of transactions in this
 * bucket will give you an indication of the lowest feerate you can put on a
 * transaction and still have a sufficiently high chance of being confirmed
 * within your desired 5 blocks.
 *
 * Here is a brief description of the implementation:
 * When a transaction enters the mempool, we track the height of the block chain
 * at entry.  All further calculations are conducted only on this set of "seen"
 * transactions. Whenever a block comes in, we count the number of transactions
 * in each bucket and the total amount of feerate paid in each bucket. Then we
 * calculate how many blocks Y it took each transaction to be mined.  We convert
 * from a number of blocks to a number of periods Y' each encompassing "scale"
 * blocks.  This is tracked in 3 different data sets each up to a maximum
 * number of periods. Within each data set we have an array of counters in each
 * feerate bucket and we increment all the counters from Y' up to max periods
 * representing that a tx was successfully confirmed in less than or equal to
 * that many periods. We want to save a history of this information, so at any
 * time we have a counter of the total number of transactions that happened in a
 * given feerate bucket and the total number that were confirmed in each of the
 * periods or less for any bucket.  We save this history by keeping an
 * exponentially decaying moving average of each one of these stats.  This is
 * done for a different decay in each of the 3 data sets to keep relevant data
 * from different time horizons.  Furthermore we also keep track of the number
 * unmined (in mempool or left mempool without being included in a block)
 * transactions in each bucket and for how many blocks they have been
 * outstanding and use both of these numbers to increase the number of transactions
 * we've seen in that feerate bucket when calculating an estimate for any number
 * of confirmations below the number of blocks they've been outstanding.
 *
 *  We want to be able to estimate feerates that are needed on tx's to be included in
 * a certain number of blocks.  Every time a block is added to the best chain, this class records
 * stats on the transactions included in that block
 */

type BlockPolicyEstimator struct {
	nBestSeenHeight     uint
	firstRecordedHeight uint
	historicalFirst     uint
	historicalBest      uint
	mapMemPoolTxs       map[string]TxStatsInfo // map of txids to information about that transaction

	feeStats   *TxConfirmStats
	shortStats *TxConfirmStats
	longStats  *TxConfirmStats

	trackedTxs   uint
	untrackedTxs uint

	buckets   []float64
	bucketMap map[float64]int
}

func NewBlockPolicyEstimator() *BlockPolicyEstimator {
	if MinBucketFeeRate <= 0 {
		panic("MinBucketFeeRate must no be 0")
	}

	bucketIndex := 0
	buckets := make([]float64, 0)
	bucketsMap := make(map[float64]int)
	for bucketBoundary := MinBucketFeeRate; bucketBoundary <= MaxBucketFeeRate; bucketIndex++ {
		buckets = append(buckets, bucketBoundary)
		bucketsMap[bucketBoundary] = bucketIndex
		bucketBoundary *= FeeSpacing
	}

	buckets = append(buckets, InfFeeRate)
	bucketsMap[InfFeeRate] = bucketIndex
	if len(bucketsMap) != len(buckets) {
		panic("bucketsMap and buckets not same size")
	}

	feeStats := NewTxConfirmStats(buckets, bucketsMap, MedBlockPeriods, MedDecay, MedScale)
	shortStats := NewTxConfirmStats(buckets, bucketsMap, ShortBlockPeriods, ShortDecay, ShortScale)
	longStats := NewTxConfirmStats(buckets, bucketsMap, LongBlockPeriods, LongDecay, LongScale)
	return &BlockPolicyEstimator{
		feeStats:   feeStats,
		shortStats: shortStats,
		longStats:  longStats,
		bucketMap:  bucketsMap,
		buckets:    buckets,
	}
}

type MempoolTx struct {
	hash   string
	height uint
	size   int
	fee    float64
}

type FeeRate struct {
	nSatoshisPerK float64 // unit is satoshis-per-1,000-bytes
}

func NewFeeRate(fee float64, size int) *FeeRate {
	nSatoshisPerK := float64(0)
	if size > 0 {
		nSatoshisPerK = fee * 1000 / float64(size)
	}

	return &FeeRate{
		nSatoshisPerK: nSatoshisPerK,
	}
}

func (r *FeeRate) GetFee(nBytes int) float64 {
	nFee := r.nSatoshisPerK * float64(nBytes) / 1000.0
	if nFee == 0 && nBytes != 0 {
		if r.nSatoshisPerK > 0 {
			nFee = float64(1)
		}

		if r.nSatoshisPerK < 0 {
			nFee = -1
		}
	}

	return nFee
}

func (r *FeeRate) GetFeePerK() float64 {
	return r.GetFee(1000)
}

func (e *BlockPolicyEstimator) ProcessTransaction(entry *MempoolTx, validFeeEstimate bool) {
	if _, ok := e.mapMemPoolTxs[entry.hash]; ok {
		log.Printf("Blockpolicy error mempool tx %s already being tracked\n", entry.hash)
	}

	if entry.height != e.nBestSeenHeight {
		// Ignore side chains and re-orgs; assuming they are random they don't
		// affect the estimate.  We'll potentially double count transactions in 1-block reorgs.
		// Ignore txs if BlockPolicyEstimator is not in sync with chainActive.Tip().
		// It will be synced next time a block is processed.
		return
	}

	// Only want to be updating estimates when our blockchain is synced,
	// otherwise we'll miscalculate how many blocks its taking to get included.
	if !validFeeEstimate {
		e.untrackedTxs++
		return
	}
	e.trackedTxs++

	// Feerates are stored and reported as BTC-per-kb:
	feeRate := NewFeeRate(entry.fee, entry.size)
	stats := TxStatsInfo{
		blockHeight: entry.height,
	}
	bucketIndex := e.feeStats.NewTx(entry.height, feeRate.GetFeePerK())
	stats.bucketIndex = bucketIndex
	bucketIndex2 := e.shortStats.NewTx(entry.height, feeRate.GetFeePerK())
	if bucketIndex != bucketIndex2 {
		panic("bucketIndex != bucketIndex2")
	}
	bucketIndex3 := e.longStats.NewTx(entry.height, feeRate.GetFeePerK())
	if bucketIndex != bucketIndex3 {
		panic("bucketIndex != bucketIndex3")
	}

	e.mapMemPoolTxs[entry.hash] = stats
}

// This function is called from CTxMemPool::removeUnchecked to ensure
// txs removed from the mempool for any reason are no longer
// tracked. Txs that were part of a block have already been removed in
// processBlockTx to ensure they are never double tracked, but it is
// of no harm to try to remove them again.
func (e *BlockPolicyEstimator) removeTx(hash string, inBlock bool) bool {

	val, ok := e.mapMemPoolTxs[hash]
	if !ok {
		return false
	}

	e.feeStats.RemoveTx(val.blockHeight, e.nBestSeenHeight, val.bucketIndex, inBlock)
	e.shortStats.RemoveTx(val.blockHeight, e.nBestSeenHeight, val.bucketIndex, inBlock)
	e.longStats.RemoveTx(val.blockHeight, e.nBestSeenHeight, val.bucketIndex, inBlock)
	delete(e.mapMemPoolTxs, hash)

	return true
}

func (e *BlockPolicyEstimator) processBlockTx(nBlockHeight uint, entry *MempoolTx) bool {
	if !e.removeTx(entry.hash, true) {
		// This transaction wasn't being tracked for fee estimation
		return false
	}

	// How many blocks did it take for miners to include this transaction?
	// blocksToConfirm is 1-based, so a transaction included in the earliest
	// possible block has confirmation count of 1
	blocksToConfirm := nBlockHeight - entry.height
	if blocksToConfirm <= 0 {
		// This can't happen because we don't process transactions from a block with a height
		// lower than our greatest seen height
		log.Println("Blockpolicy error Transaction had negative blocksToConfirm")
		return false
	}

	// Feerates are stored and reported as BTC-per-kb:
	feeRate := NewFeeRate(entry.fee, entry.size)

	e.feeStats.Record(blocksToConfirm, feeRate.GetFeePerK())
	e.shortStats.Record(blocksToConfirm, feeRate.GetFeePerK())
	e.longStats.Record(blocksToConfirm, feeRate.GetFeePerK())
	return true
}

func (e *BlockPolicyEstimator) processBlock(nBlockHeight uint, entries []*MempoolTx) {
	if nBlockHeight <= e.nBestSeenHeight {
		// Ignore side chains and re-orgs; assuming they are random
		// they don't affect the estimate.
		// And if an attacker can re-org the chain at will, then
		// you've got much bigger problems than "attacker can influence
		// transaction fees."
		return
	}

	// Must update nBestSeenHeight in sync with ClearCurrent so that
	// calls to removeTx (via processBlockTx) correctly calculate age
	// of unconfirmed txs to remove from tracking.
	e.nBestSeenHeight = nBlockHeight

	// Update unconfirmed circular buffer
	e.feeStats.ClearCurrent(nBlockHeight)
	e.shortStats.ClearCurrent(nBlockHeight)
	e.longStats.ClearCurrent(nBlockHeight)

	// Decay all exponential averages
	e.feeStats.UpdateMovingAverages()
	e.shortStats.UpdateMovingAverages()
	e.longStats.UpdateMovingAverages()

	countedTxs := 0

	// Update averages with data points from current block
	for _, entry := range entries {
		if e.processBlockTx(nBlockHeight, entry) {
			countedTxs++
		}
	}

	if e.firstRecordedHeight == 0 && countedTxs > 0 {
		e.firstRecordedHeight = e.nBestSeenHeight
		log.Printf("Blockpolicy first recorded height %v\n", e.firstRecordedHeight)
	}

	//  log.Printf("Blockpolicy estimates updated by %u of %u block txs, since last block %u of %u tracked, mempool map size %u, max target %u from %s\n",
	//          countedTxs, entries.size(), trackedTxs, trackedTxs + untrackedTxs, mapMemPoolTxs.size(),
	//          MaxUsableEstimate(), HistoricalBlockSpan() > BlockSpan() ? "historical" : "current");

	e.trackedTxs = 0
	e.untrackedTxs = 0
}

const (
	ShortHalflife  FeeEstimateHorizon = 0
	MediumHalflife FeeEstimateHorizon = 1
	LongHalflife   FeeEstimateHorizon = 2
)

type FeeEstimateHorizon int

func (e *BlockPolicyEstimator) estimateRawFee(confTarget uint, successThreshold float64, horizon FeeEstimateHorizon) (*FeeRate, *EstimationResult) {
	var stats *TxConfirmStats
	sufficientTxs := SufficientFeeTxs
	switch horizon {
	case ShortHalflife:
		stats = e.shortStats
		sufficientTxs = SufficientTxsShort
		break
	case MediumHalflife:
		stats = e.feeStats
		break
	case LongHalflife:
		stats = e.longStats
		break
	default:
		panic("unknown FeeEstimateHorizon")
	}

	// Return failure if trying to analyze a target we're not tracking
	if confTarget <= 0 || confTarget > stats.GetMaxConfirms() {
		return NewFeeRate(0, 0), nil
	}
	if successThreshold > 1 {
		return NewFeeRate(0, 0), nil
	}

	result, median := stats.EstimateMedianVal(confTarget, sufficientTxs, successThreshold, true, uint(e.nBestSeenHeight))
	if median < 0 {
		return NewFeeRate(0, 0), nil
	}

	return NewFeeRate(median, 0), result //TODO round median
}

func (e *BlockPolicyEstimator) estimateFee(confTarget uint) (*FeeRate, *EstimationResult) {
	// It's not possible to get reasonable estimates for confTarget of 1
	if confTarget <= 1 {
		return NewFeeRate(0, 0), nil
	}

	return e.estimateRawFee(confTarget, DoubleSuccessPct, MediumHalflife)
}

/** Return a fee estimate at the required successThreshold from the shortest
 * time horizon which tracks confirmations up to the desired target.  If
 * checkShorterHorizon is requested, also allow short time horizon estimates
 * for a lower target to reduce the given answer */

func (e *BlockPolicyEstimator) estimateCombinedFee(confTarget uint, successThreshold float64, checkShorterHorizon bool) (float64, *FeeRate, *EstimationResult) {
	estimate := float64(-1)
	var result *EstimationResult
	if confTarget >= 1 && confTarget <= e.longStats.GetMaxConfirms() {
		// Find estimate from shortest time horizon possible
		if confTarget <= e.shortStats.GetMaxConfirms() { // short horizon
			result, estimate = e.shortStats.EstimateMedianVal(confTarget, SufficientTxsShort, successThreshold, true, e.nBestSeenHeight)
		} else if confTarget <= e.feeStats.GetMaxConfirms() { // medium horizon
			result, estimate = e.feeStats.EstimateMedianVal(confTarget, SufficientFeeTxs, successThreshold, true, e.nBestSeenHeight)
		} else { // long horizon
			result, estimate = e.longStats.EstimateMedianVal(confTarget, SufficientFeeTxs, successThreshold, true, e.nBestSeenHeight)
		}
		if checkShorterHorizon {
			// If a lower confTarget from a more recent horizon returns a lower answer use it.
			if confTarget > e.feeStats.GetMaxConfirms() {
				tempResult, medMax := e.feeStats.EstimateMedianVal(e.feeStats.GetMaxConfirms(), SufficientFeeTxs, successThreshold, true, e.nBestSeenHeight)
				if medMax > 0 && (estimate == -1 || medMax < estimate) {
					estimate = medMax
					result = tempResult
				}
			}
			if confTarget > e.shortStats.GetMaxConfirms() {
				tempResult, shortMax := e.shortStats.EstimateMedianVal(e.shortStats.GetMaxConfirms(), SufficientTxsShort, successThreshold, true, e.nBestSeenHeight)
				if shortMax > 0 && (estimate == -1 || shortMax < estimate) {
					estimate = shortMax
					result = tempResult
				}
			}
		}
	}

	return estimate, NewFeeRate(estimate, 0), result
}

/** Ensure that for a conservative estimate, the DOUBLE_SUCCESS_PCT is also met
 * at 2 * target for any longer time horizons.
 */
func (e *BlockPolicyEstimator) estimateConservativeFee(doubleTarget uint) (float64, *FeeRate, *EstimationResult) {
	estimate := float64(-1)
	var result *EstimationResult
	if doubleTarget <= e.shortStats.GetMaxConfirms() {
		result, estimate = e.feeStats.EstimateMedianVal(doubleTarget, SufficientFeeTxs, DoubleSuccessPct, true, e.nBestSeenHeight)
	}
	if doubleTarget <= e.feeStats.GetMaxConfirms() {
		tempResult, longEstimate := e.longStats.EstimateMedianVal(doubleTarget, SufficientFeeTxs, DoubleSuccessPct, true, e.nBestSeenHeight)
		if longEstimate > estimate {
			estimate = longEstimate
			result = tempResult
		}
	}

	return estimate, NewFeeRate(estimate, 0), result
}

type FeeReason int

const (
	HalfEstimate   FeeReason = 1
	FullEstimate   FeeReason = 2
	DoubleEstimate FeeReason = 3
	Conservative   FeeReason = 4
)

type FeeCalculation struct {
	est            *EstimationResult
	reason         FeeReason
	desiredTarget  uint
	returnedTarget uint
}

func (e *BlockPolicyEstimator) BlockSpan() uint {
	if e.firstRecordedHeight == 0 {
		return 0
	}

	if e.nBestSeenHeight < e.firstRecordedHeight {
		panic("historicalBest < historicalFirst")
	}

	return e.nBestSeenHeight - e.firstRecordedHeight
}

func (e *BlockPolicyEstimator) HistoricalBlockSpan() uint {
	if e.historicalFirst == 0 {
		return 0
	}
	if e.historicalBest < e.historicalFirst {
		panic("historicalBest < historicalFirst")
	}

	if e.nBestSeenHeight-e.historicalBest > OldestEstimateHistory {
		return 0
	}

	return e.historicalBest - e.historicalFirst
}

func (e *BlockPolicyEstimator) MaxUsableEstimate() uint {
	// Block spans are divided by 2 to make sure there are enough potential failing data points for the estimate
	return MinU(e.longStats.GetMaxConfirms(), MaxU(e.BlockSpan(), e.HistoricalBlockSpan())/2)
}

/** estimateSmartFee returns the max of the feerates calculated with a 60%
 * threshold required at target / 2, an 85% threshold required at target and a
 * 95% threshold required at 2 * target.  Each calculation is performed at the
 * shortest time horizon which tracks the required target.  Conservative
 * estimates, however, required the 95% threshold at 2 * target be met for any
 * longer time horizons also.
 */
func (e *BlockPolicyEstimator) estimateSmartFee(confTarget uint, conservative bool) (float64, *FeeRate, *EstimationResult) {

	feeCalc := &FeeCalculation{
		desiredTarget:  confTarget,
		returnedTarget: confTarget,
	}

	median := float64(-1)

	// Return failure if trying to analyze a target we're not tracking
	if confTarget <= 0 || confTarget > e.longStats.GetMaxConfirms() {
		return 0, NewFeeRate(0, 0), nil
	}

	// It's not possible to get reasonable estimates for confTarget of 1
	if confTarget == 1 {
		confTarget = 2
	}

	maxUsableEstimate := e.MaxUsableEstimate()
	if confTarget > maxUsableEstimate {
		confTarget = maxUsableEstimate
	}
	feeCalc.returnedTarget = confTarget

	if confTarget <= 1 {
		return 0, NewFeeRate(0, 0), nil //error condition
	}

	/** true is passed to estimateCombined fee for target/2 and target so
	 * that we check the max confirms for shorter time horizons as well.
	 * This is necessary to preserve monotonically increasing estimates.
	 * For non-conservative estimates we do the same thing for 2*target, but
	 * for conservative estimates we want to skip these shorter horizons
	 * checks for 2*target because we are taking the max over all time
	 * horizons so we already have monotonically increasing estimates and
	 * the purpose of conservative estimates is not to let short term
	 * fluctuations lower our estimates by too much.
	 */
	halfEst, _, tempResult := e.estimateCombinedFee(confTarget/2, HalfSuccessPct, true)
	feeCalc.est = tempResult
	feeCalc.reason = HalfEstimate
	median = halfEst

	actualEst, _, tempResult := e.estimateCombinedFee(confTarget, SuccessPct, true)
	if actualEst > median {
		median = actualEst
		feeCalc.est = tempResult
		feeCalc.reason = FullEstimate
	}

	doubleEst, _, tempResult := e.estimateCombinedFee(2*confTarget, DoubleSuccessPct, !conservative)
	if doubleEst > median {
		median = doubleEst
		feeCalc.est = tempResult
		feeCalc.reason = DoubleEstimate
	}

	if conservative || median == -1 {
		consEst, _, tempResult := e.estimateConservativeFee(2 * confTarget)
		if consEst > median {
			median = consEst
			feeCalc.est = tempResult
			feeCalc.reason = Conservative
		}
	}

	if median < 0 {
		return 0, NewFeeRate(0, 0), nil //error condition
	}

	return median, NewFeeRate(median, 0), tempResult
}
