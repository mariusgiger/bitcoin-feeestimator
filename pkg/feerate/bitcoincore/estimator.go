package bitcoincore

//Consider https://mrekucci.blogspot.com/2015/07/dont-abuse-mathmax-mathmin.html
import (
	"log"
)

type Estimator interface {
	removeTx(hash string, inBlock int) bool
	processNewMempoolTransactions(rawMempoolTransactions map[string]string)
	addTransactionToMempool(transaction string, isValidFeeEstimate bool)
	processBlockTx(blockHeight int, transaction string)
	processBlock(blockHeight int, txIds []string)
	blockSpan()
	historicalBlockSpan()
	maxUsableEstimate()
	estimateSmartFee(confirmationTarget int, conservative bool)
	estimateCombinedFee(confirmationTarget int, successTreshold float32, checkShorterHorizon bool)
	estimateConservativeFee(confirmationTarget int)
}

type BitcoreEstimator struct {
	feeStats            *TransactionStats
	shortStats          *TransactionStats
	longStats           *TransactionStats
	untrackedTxs        int
	trackedTxs          int
	bestSeenHeight      int
	historicalFirst     int
	historicalBest      int
	firstRecordedHeight int
	mempoolTransactions map[string]MempoolTransaction
	buckets             []int
}

func NewBitcoreEstimator(bestSeenHeight int, firstRecordedHeight int, historicalFirst int, historicalBest int, trackedTxs int, untrackedTxs int) *BitcoreEstimator {
	estimator := new(BitcoreEstimator)
	estimator.buckets = make([]int, 0)
	estimator.bestSeenHeight = bestSeenHeight
	estimator.firstRecordedHeight = firstRecordedHeight
	estimator.historicalFirst = historicalFirst
	estimator.historicalBest = historicalBest
	estimator.trackedTxs = trackedTxs
	estimator.untrackedTxs = untrackedTxs

	//     for (let bucketBoundary = MIN_BUCKET_FEERATE; bucketBoundary <= MAX_BUCKET_FEERATE; bucketBoundary *= FEE_SPACING) {
	//       this.buckets.push(bucketBoundary);
	//     }
	//estimator.buckets.push(INF_FEERATE);
	estimator.feeStats = NewTransactionStats(estimator.buckets, MED_BLOCK_PERIODS, MED_DECAY, MED_SCALE)
	estimator.shortStats = NewTransactionStats(estimator.buckets, SHORT_BLOCK_PERIODS, SHORT_DECAY, SHORT_SCALE)
	estimator.longStats = NewTransactionStats(estimator.buckets, LONG_BLOCK_PERIODS, LONG_DECAY, LONG_SCALE)

	estimator.mempoolTransactions = make(map[string]MempoolTransaction)
	return estimator
}

func (be BitcoreEstimator) removeTx(hash string, inBlock bool) bool {
	transaction := be.mempoolTransactions[hash]
	//TODO extract last added transaction
	//lastAddedTransactionHash = Array.from(this.mempoolTransactions.keys())[this.mempoolTransactions.size - 1];
	isLastAdded := false //lastAddedTransactionHash == hash
	// todo: Why should it make any sense?
	if !isLastAdded {
		be.feeStats.removeTx(transaction.BlockHeight, be.bestSeenHeight, transaction.BucketIndex, inBlock)
		be.shortStats.removeTx(transaction.BlockHeight, be.bestSeenHeight, transaction.BucketIndex, inBlock)
		be.longStats.removeTx(transaction.BlockHeight, be.bestSeenHeight, transaction.BucketIndex, inBlock)
		delete(be.mempoolTransactions, hash)
		return true
	}

	return false
}

func (be BitcoreEstimator) processNewMempoolTransactions(rawMempoolTransactions map[string]Transaction) {
	keys := make([]string, 0, len(rawMempoolTransactions))
	for k := range rawMempoolTransactions {
		keys = append(keys, k)
	}

	for _, key := range keys {
		transaction := rawMempoolTransactions[key]
		transaction.hash = key
		be.addTransactionToMempool(transaction, true)
	}
}

/**
 * Adds transaction to mempool.
 * Notice: It is important to process blocks before adding new mempool transactions, because
 * only transactions which entered mempool at same height as last block will be processed.
 * @param transaction mempoolTransactionEntry
 * @param isValidFeeEstimate
 * NOTICE: transaction should have 'hash' property, which is not in raw mempool data
 */
func (be BitcoreEstimator) addTransactionToMempool(transaction Transaction, isValidFeeEstimate bool) {
	_, present := be.mempoolTransactions[transaction.hash]
	if present {
		return
	}

	if transaction.height != be.bestSeenHeight {
		// Ignore side chains and re-orgs; assuming they are random they don't
		// affect the estimate.  We'll potentially double count transactions in 1-block reorgs.
		// Ignore txs if Estimator is not in sync with chainActive.Tip().
		// It will be synced next time a block is processed.
		return
	}

	// Only want to be updating estimates when our blockchain is synced,
	// otherwise we'll miscalculate how many blocks its taking to get included.
	if !isValidFeeEstimate {
		be.untrackedTxs++
		return
	}

	be.trackedTxs++
	// Fee rates are stored and reported as BTC-per-kb:
	feeRate := NewFeeRate(transaction.fee, transaction.size)
	bucketIndex := be.feeStats.addTx(transaction.height, feeRate.GetFeePerK())
	be.shortStats.addTx(transaction.height, feeRate.GetFeePerK())
	be.longStats.addTx(transaction.height, feeRate.GetFeePerK())
	be.mempoolTransactions[transaction.hash] = NewMempoolTransaction(transaction, bucketIndex)
}

func (be BitcoreEstimator) processBlockTx(blockHeight int, transaction MempoolTransaction) bool {
	if !be.removeTx(transaction.Hash, true) {
		// This transaction wasn't being tracked for fee estimation
		return false
	}

	// How many blocks did it take for miners to include this transaction?
	// blocksToConfirm is 1-based, so a transaction included in the earliest
	// possible block has confirmation count of 1
	blocksToConfirm := blockHeight - transaction.Height
	if blocksToConfirm <= 0 {
		// This can't happen because we don't process transactions from a block with a height
		// lower than our greatest seen height
		return false
	}

	// Fee rates are stored and reported as BTC-per-kb:
	feeRate := NewFeeRate(transaction.Fee, transaction.Size)

	be.feeStats.record(blocksToConfirm, feeRate.GetFeePerK())
	be.shortStats.record(blocksToConfirm, feeRate.GetFeePerK())
	be.longStats.record(blocksToConfirm, feeRate.GetFeePerK())
	return true
}

func (be BitcoreEstimator) processBlock(blockHeight int, txids []string) {
	if blockHeight <= be.bestSeenHeight {
		// Ignore side chains and re-orgs; assuming they are random
		// they don't affect the estimate.
		// And if an attacker can re-org the chain at will, then
		// you've got much bigger problems than "attacker can influence
		// transaction fees."
		return
	}

	// Must update bestSeenHeight in sync with ClearCurrent so that
	// calls to removeTx (via processBlockTx) correctly calculate age
	// of unconfirmed txs to remove from tracking.
	be.bestSeenHeight = blockHeight

	// Update unconfirmed circular buffer
	be.feeStats.clearCurrent(blockHeight)
	be.shortStats.clearCurrent(blockHeight)
	be.longStats.clearCurrent(blockHeight)

	// Decay all exponential averages
	be.feeStats.updateMovingAverages()
	be.shortStats.updateMovingAverages()
	be.longStats.updateMovingAverages()

	// Update averages with data points from current block
	for i := 0; i < len(txids); i++ {
		tx, prs := be.mempoolTransactions[txids[i]]
		if prs {
			be.processBlockTx(blockHeight, tx)
		}
	}
}

func (be BitcoreEstimator) blockSpan() int {
	if be.firstRecordedHeight == 0 {
		return 0
	}

	if be.bestSeenHeight < be.firstRecordedHeight {
		panic("First recorded height can not me bigger than last seen height")
	}

	return be.bestSeenHeight - be.firstRecordedHeight
}

func (be BitcoreEstimator) historicalBlockSpan() int {
	if be.historicalFirst == 0 {
		return 0
	}

	if be.historicalBest < be.historicalFirst {
		panic("First recorded historical height can not me bigger than last seen historical height")
	}

	if be.bestSeenHeight-be.historicalBest > OLDEST_ESTIMATE_HISTORY {
		return 0
	}

	return be.historicalBest - be.historicalFirst
}

func (be BitcoreEstimator) maxUsableEstimate() int {
	// Block spans are divided by 2 to make sure there are enough potential failing data points for the estimate
	maxBlockSpan := Max(be.blockSpan(), be.historicalBlockSpan()) / 2
	return Min(int(be.longStats.getMaxConfirms()), maxBlockSpan)
}

func (be BitcoreEstimator) estimateSmartFee(confirmationTarget int, isConservative bool) (*FeeRate, error) {
	target := confirmationTarget
	feeCalculation := new(FeeCalculation)
	feeCalculation.DesiredTarget = target
	feeCalculation.ReturnedTarget = target

	median := 0
	halfEst := -1
	actualEst := -1
	doubleEst := -1
	consEst := -1
	var estimationResult *EstimationResult

	// Return failure if trying to analyze a target we're not tracking
	if target <= 0 || target > be.longStats.getMaxConfirms() {
		return nil, nil //TODO return error
	}

	// It's not possible to get reasonable estimates for confTarget of 1
	if target == 1 {
		target = 2
	}

	maxUsableEstimate := be.maxUsableEstimate()
	if target > maxUsableEstimate {
		target = maxUsableEstimate
	}

	if target <= 1 {
		log.Print("Target is to small or we do not have enough data")
		return nil, nil //TODO return error
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

	halfEst, estimationResult = be.estimateCombinedFee(int(target/2), HALF_SUCCESS_PCT, true)
	feeCalculation.Est = estimationResult
	feeCalculation.Reason = HALF_ESTIMATE

	median = halfEst
	actualEst, estimationResult = be.estimateCombinedFee(target, SUCCESS_PCT, true)
	if actualEst > median {
		median = actualEst
		feeCalculation.Est = estimationResult
		feeCalculation.Reason = FULL_ESTIMATE
	}

	doubleEst, estimationResult = be.estimateCombinedFee(2*target, DOUBLE_SUCCESS_PCT, !isConservative)
	if doubleEst > median {
		median = doubleEst
		feeCalculation.Est = estimationResult
		feeCalculation.Reason = DOUBLE_ESTIMATE
	}

	if isConservative || median == -1 {
		consEst, estimationResult = be.estimateConservativeFee(2 * target)
		if consEst > median {
			median = consEst
			feeCalculation.Est = estimationResult
			feeCalculation.Reason = CONSERVATIVE
		}
	}

	log.Printf("fee estimation: %v", feeCalculation)
	if median < 0 {
		// error condition
		return nil, nil //TODO return error
	}

	return FromSatoshisPerK(float64(median)), nil
}

// Return a fee estimate at the required successThreshold from the shortest
// time horizon which tracks confirmations up to the desired target.  If
// checkShorterHorizon is requested, also allow short time horizon estimates
// for a lower target to reduce the given answer
func (be BitcoreEstimator) estimateCombinedFee(confirmationTarget int, successThreshold float64, checkShorterHorizon bool) (int, *EstimationResult) {
	estimate := -1
	var result *EstimationResult

	// Find estimate from shortest time horizon possible
	if confirmationTarget >= 1 && confirmationTarget <= be.longStats.getMaxConfirms() {
		if confirmationTarget <= be.shortStats.getMaxConfirms() { //short horizon
			estimate, result = be.shortStats.estimateMedianVal(confirmationTarget, SUFFICIENT_TXS_SHORT, successThreshold, true, be.bestSeenHeight)
		} else if confirmationTarget <= be.feeStats.getMaxConfirms() { // medium horizon
			estimate, result = be.feeStats.estimateMedianVal(confirmationTarget, SUFFICIENT_TXS_SHORT, successThreshold, true, be.bestSeenHeight)
		} else { //longHorizon
			estimate, result = be.longStats.estimateMedianVal(confirmationTarget, SUFFICIENT_TXS_SHORT, successThreshold, true, be.bestSeenHeight)
		}

		if checkShorterHorizon {
			// If a lower confTarget from a more recent horizon returns a lower answer use it.
			if confirmationTarget > be.feeStats.getMaxConfirms() {
				confirms := int(be.feeStats.getMaxConfirms()) //TODO rounding?
				medMax, tempResult := be.feeStats.estimateMedianVal(confirms, SUFFICIENT_FEETXS, successThreshold, true, be.bestSeenHeight)
				if medMax > 0 && (estimate == -1 || medMax < estimate) {
					estimate = medMax
					if result != nil { //TODO why only if the result is not set
						result = tempResult
					}
				}
			}

			if confirmationTarget > be.shortStats.getMaxConfirms() {
				confirms := int(be.shortStats.getMaxConfirms()) //TODO rounding?
				shortMax, tempResult := be.shortStats.estimateMedianVal(confirms, SUFFICIENT_FEETXS, successThreshold, true, be.bestSeenHeight)
				if shortMax > 0 && (estimate == -1 || shortMax < estimate) {
					estimate = shortMax
					if result != nil { //TODO why only if the result is not set
						result = tempResult
					}
				}
			}
		}
	}

	return estimate, result
}

func (be BitcoreEstimator) estimateConservativeFee(doubleTarget int) (int, *EstimationResult) {
	estimate := -1
	longEstimate := -1
	var result *EstimationResult
	var longResult *EstimationResult

	if doubleTarget <= be.shortStats.getMaxConfirms() {
		estimate, result = be.feeStats.estimateMedianVal(doubleTarget, SUFFICIENT_FEETXS, DOUBLE_SUCCESS_PCT, true, be.bestSeenHeight)
	}

	if doubleTarget <= be.feeStats.getMaxConfirms() {
		longEstimate, longResult = be.longStats.estimateMedianVal(doubleTarget, SUFFICIENT_FEETXS, DOUBLE_SUCCESS_PCT, true, be.bestSeenHeight)
		if longEstimate > estimate {
			estimate = longEstimate
			result = longResult
		}
	}

	return estimate, result
}
