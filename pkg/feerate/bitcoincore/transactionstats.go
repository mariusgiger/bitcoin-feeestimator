package bitcoincore

import (
	"log"
)

type TransactionStats struct {
	scale                          int
	confirmationsPerBlock          [][]int
	buckets                        []int
	unconfirmedTransactions        [][]int
	oldUnconfirmedTransactions     []int
	decay                          float64
	failAverage                    [][]int
	feeSumPerBucket                []float64
	maxPeriods                     int
	confirmedTransactionsPerBucket []int
}

func NewTransactionStats(buckets []int, maxPeriods int, decay float64, scale int) *TransactionStats {
	if scale == 0 {
		panic("scale must be non-zero")
	}

	stats := new(TransactionStats)
	stats.buckets = buckets
	stats.decay = decay
	stats.scale = scale
	stats.maxPeriods = maxPeriods

	/**
	 * For each bucket X:
	 * Count the total # of txs in each bucket
	 * Track the historical moving feeSumPerBucket of this total over blocks
	 */
	stats.confirmedTransactionsPerBucket = make([]int, len(stats.buckets))

	/**
	 * Count the total # of txs confirmed within Y blocks in each bucket
	 * Track the historical moving feeSumPerBucket of theses totals over blocks
	 */
	stats.confirmationsPerBlock = MakeSlice2D(maxPeriods, len(stats.buckets))

	/**
	 * Track moving avg of txs which have been evicted from the mempool
	 * after failing to be confirmed within Y blocks
	 */
	stats.failAverage = MakeSlice2D(maxPeriods, len(stats.buckets))

	/**
	 * Sum the total feerate of all tx's in each bucket
	 * Track the historical moving feeSumPerBucket of this total over blocks
	 */
	stats.feeSumPerBucket = make([]float64, len(stats.buckets))

	/**
	 * Mempool counts of outstanding transactions
	 * For each bucket X, track the number of transactions in the mempool
	 * that are unconfirmed for each possible confirmation value Y
	 */
	stats.unconfirmedTransactions = MakeSlice2D(stats.getMaxConfirms(), len(stats.buckets))

	/**
	 * Transactions count still unconfirmed after GetMaxConfirms for each bucket.
	 * So array index is bucket index, and value is transactions count.
	 */
	stats.oldUnconfirmedTransactions = make([]int, len(stats.buckets))
	return stats
}

/**
 * Records that transaction has been confirmed
 * @param blocksToConfirm
 * @param val - fee in satoshis per kilobyte
 */
func (ts TransactionStats) record(blocksToConfirm int, val float64) {
	// blocksToConfirm is 1-based
	if blocksToConfirm < 1 {
		return
	}
	periodsToConfirm := (blocksToConfirm + ts.scale - 1) / ts.scale
	//TODO error handling if bucket index is -1
	bucketIndex := lowerBound(ts.buckets, int(val))
	for i := periodsToConfirm; i <= len(ts.confirmationsPerBlock); i++ {
		ts.confirmationsPerBlock[i-1][bucketIndex]++
	}
	ts.confirmedTransactionsPerBucket[bucketIndex]++
	ts.feeSumPerBucket[bucketIndex] += val
}

func (ts TransactionStats) removeTx(transactionHeight int, bestSeenHeight int, bucketIndex int, inBlock bool) {
	// bestSeenHeight is not updated yet for the new block
	blocksAgo := bestSeenHeight - transactionHeight
	// the Estimator hasn't seen any blocks yet
	if bestSeenHeight == 0 {
		blocksAgo = 0
	}

	if blocksAgo < 0 {
		panic("Blockpolicy error, blocks ago is negative for mempool tx")
	}

	if blocksAgo >= len(ts.unconfirmedTransactions) {
		if ts.oldUnconfirmedTransactions[bucketIndex] > 0 {
			ts.oldUnconfirmedTransactions[bucketIndex]--
		} else {
			log.Printf("Mempool tx removed from > %v blocks, bucketIndex = %v already", blocksAgo, bucketIndex)
		}
	} else {
		blockIndex := transactionHeight % len(ts.unconfirmedTransactions)
		if ts.unconfirmedTransactions[blockIndex][bucketIndex] > 0 {
			ts.unconfirmedTransactions[blockIndex][bucketIndex]--
		} else {
			log.Printf("Can't remove tx: transactions at blockIndex = %v, bucketIndex = %v already empty", blockIndex, bucketIndex)
		}
	}

	// Only counts as a failure if not confirmed for entire period
	if !inBlock && blocksAgo >= ts.scale {
		periodsAgo := blocksAgo / ts.scale
		for i := 0; i < periodsAgo && i < len(ts.failAverage); i++ {
			ts.failAverage[i][bucketIndex]++
		}
	}
}

/**
 * Add data about transaction to unconfirmed transactions
 * @param blockHeight - height when transaction entered mempool
 * @param feeInSatoshisPerK - fee in satoshis per kilobyte
 */
func (ts TransactionStats) addTx(blockHeight int, feeInSatoshisPerK float64) int {
	bucketIndex := lowerBound(ts.buckets, int(feeInSatoshisPerK)) //TODO overflow
	blockIndex := blockHeight % len(ts.unconfirmedTransactions)
	ts.unconfirmedTransactions[blockIndex][bucketIndex]++
	return bucketIndex
}

func (ts TransactionStats) getMaxConfirms() int {
	return int(ts.scale) * len(ts.confirmationsPerBlock)
}

func (ts TransactionStats) estimateMedianVal(confTarget int, sufficientTxVal float64, minimumSuccessRate float64, requireLowestPossibleFee bool, blockHeight int) (int, *EstimationResult) {

	// Counters for a bucket (or range of buckets)
	confirmedTransactionCount := 0                // Number of tx's confirmed within the confTarget
	confirmedTransactionForAllTime := 0           // Total number of tx's that were ever confirmed
	transactionsWithSameTargetStillInMempool := 0 // Number of tx's still in mempool for confTarget or longer

	neverConfirmedTransactionsLeavedMempool := 0 // Number of tx's that were never confirmed but removed from the mempool after confTarget
	periodTarget := (confTarget + ts.scale - 1) / ts.scale
	bucketsCount := len(ts.buckets) - 1

	// requireLowestPossibleFee means we are looking for the lowest feerate such that all higher
	// values pass, so we start at maxbucketindex (highest feerate) and look at successively
	// smaller buckets until we reach failure.  Otherwise, we are looking for the highest
	// feerate such that all lower values fail, and we go in the opposite direction.
	startBucket := 0
	step := 1
	if requireLowestPossibleFee {
		startBucket = bucketsCount
		step = -1
	}

	// We'll combine buckets until we have enough samples.
	// The near and far variables will define the range we've combined
	// The best variables are the last range we saw which still had a high
	// enough confirmation rate to count as success.
	// The cur variables are the current range we're counting.
	curNearBucket := startBucket
	bestNearBucket := startBucket
	curFarBucket := startBucket
	bestFarBucket := startBucket

	foundAnswer := false
	bins := len(ts.unconfirmedTransactions)
	newBucketRange := true
	passing := true
	passBucket := NewEstimatorBucket(-1, -1, 0, 0, 0, 0)
	failBucket := NewEstimatorBucket(-1, -1, 0, 0, 0, 0)

	for bucketIndex := startBucket; bucketIndex >= 0 && bucketIndex <= bucketsCount; bucketIndex += step {
		if newBucketRange {
			curNearBucket = bucketIndex
			newBucketRange = false
		}

		curFarBucket = bucketIndex
		confirmedTransactionCount += ts.confirmationsPerBlock[periodTarget-1][bucketIndex]
		confirmedTransactionForAllTime += ts.confirmedTransactionsPerBucket[bucketIndex]
		neverConfirmedTransactionsLeavedMempool += ts.failAverage[periodTarget-1][bucketIndex]

		for confirmationsCount := confTarget; confirmationsCount < ts.getMaxConfirms(); confirmationsCount++ {
			transactionsWithSameTargetStillInMempool += ts.unconfirmedTransactions[Abs(blockHeight-confirmationsCount)%bins][bucketIndex]
		}

		transactionsWithSameTargetStillInMempool += ts.oldUnconfirmedTransactions[bucketIndex]
		// If we have enough transaction data points in this range of buckets,
		// we can test for success
		// (Only count the confirmed data points, so that each confirmation count
		// will be looking at the same amount of data and same bucket breaks)
		if confirmedTransactionForAllTime >= int(sufficientTxVal/(1-ts.decay)) { //TODO overflow
			curSuccessRate := float64(confirmedTransactionCount) / float64(confirmedTransactionForAllTime+neverConfirmedTransactionsLeavedMempool+transactionsWithSameTargetStillInMempool)
			// Check to see if we are no longer getting confirmed at the success rate
			lowerFeeNeeded := requireLowestPossibleFee && minimumSuccessRate > curSuccessRate
			higherSuccessRateRequired := !requireLowestPossibleFee && curSuccessRate > minimumSuccessRate

			if lowerFeeNeeded || higherSuccessRateRequired {
				if passing {
					// First time we hit a failure record the failed bucket
					failMinBucket := Min(curNearBucket, curFarBucket)
					failMaxBucket := Max(curNearBucket, curFarBucket)

					if failMinBucket != 0 {
						failBucket.Start = ts.buckets[failMinBucket-1]
					} else {
						failBucket.Start = 0
					}

					failBucket.End = ts.buckets[failMaxBucket]
					failBucket.WithinTarget = confirmedTransactionCount
					failBucket.TotalConfirmed = confirmedTransactionForAllTime
					failBucket.InMempool = transactionsWithSameTargetStillInMempool
					failBucket.LeftMempool = neverConfirmedTransactionsLeavedMempool
					passing = false
				}
				// Otherwise update the cumulative stats, and the bucket variables
				// and reset the counters
			} else {
				// Reset any failed bucket, currently passing
				failBucket = NewEstimatorBucket(-1, -1, 0, 0, 0, 0)
				foundAnswer = true
				passing = true

				passBucket.WithinTarget = confirmedTransactionCount
				passBucket.TotalConfirmed = confirmedTransactionForAllTime
				passBucket.InMempool = transactionsWithSameTargetStillInMempool
				passBucket.LeftMempool = neverConfirmedTransactionsLeavedMempool

				confirmedTransactionCount = 0
				confirmedTransactionForAllTime = 0
				neverConfirmedTransactionsLeavedMempool = 0
				transactionsWithSameTargetStillInMempool = 0

				bestNearBucket = curNearBucket
				bestFarBucket = curFarBucket
				newBucketRange = true
			}
		}
	}

	median := -1
	txSum := 0

	// Calculate the "feeSumPerBucket" feerate of the best bucket range that met success conditions
	// Find the bucket with the median transaction and then report the feeSumPerBucket feerate from that bucket
	// This is a compromise between finding the median which we can't since we don't save all tx's
	// and reporting the feeSumPerBucket which is less accurate
	minBucket := Min(bestNearBucket, bestFarBucket)
	maxBucket := Max(bestNearBucket, bestFarBucket)
	for j := minBucket; j <= maxBucket; j++ {
		txSum += ts.confirmedTransactionsPerBucket[j]
	}

	if foundAnswer && txSum != 0 {
		txSum /= 2
		for j := minBucket; j <= maxBucket; j++ {
			if ts.confirmedTransactionsPerBucket[j] < txSum {
				txSum -= ts.confirmedTransactionsPerBucket[j]
			} else { // we're in the right bucket
				median = int(ts.feeSumPerBucket[j] / float64(ts.confirmedTransactionsPerBucket[j]))
				break
			}
		}

		if minBucket != 0 {
			passBucket.Start = ts.buckets[minBucket-1]
		} else {
			passBucket.Start = 0
		}

		passBucket.End = ts.buckets[maxBucket]
	}

	// If we were passing until we reached last few buckets with insufficient data, then report those as failed
	if passing && !newBucketRange {
		failMinBucket := Min(curNearBucket, curFarBucket)
		failMaxBucket := Max(curNearBucket, curFarBucket)
		if failMinBucket != 0 {
			failBucket.Start = ts.buckets[failMinBucket-1]
		} else {
			failBucket.Start = 0
		}

		failBucket.End = ts.buckets[failMaxBucket]
		failBucket.WithinTarget = confirmedTransactionCount
		failBucket.TotalConfirmed = confirmedTransactionForAllTime
		failBucket.InMempool = transactionsWithSameTargetStillInMempool
		failBucket.LeftMempool = neverConfirmedTransactionsLeavedMempool
	}

	result := NewEstimationResult(passBucket, failBucket, ts.decay, ts.scale)
	return median, result
}

func (ts TransactionStats) clearCurrent(blockHeight int) {
	blockIndex := blockHeight % len(ts.unconfirmedTransactions)
	for j := 0; j < len(ts.buckets); j++ {
		ts.oldUnconfirmedTransactions[j] += ts.unconfirmedTransactions[blockIndex][j]
		ts.unconfirmedTransactions[blockIndex][j] = 0
	}
}

func (ts TransactionStats) updateMovingAverages() {
	for j := 0; j < len(ts.buckets); j++ {
		for i := 0; i < len(ts.confirmationsPerBlock); i++ {
			ts.confirmationsPerBlock[i][j] = int(float64(ts.confirmationsPerBlock[i][j]) * ts.decay) //TODO overflow
		}
		for i := 0; i < len(ts.failAverage); i++ {
			ts.failAverage[i][j] = int(float64(ts.failAverage[i][j]) * ts.decay) //TODO overflow
		}
		ts.feeSumPerBucket[j] = ts.feeSumPerBucket[j] * float64(ts.decay)
		ts.confirmedTransactionsPerBucket[j] = int(float64(ts.confirmedTransactionsPerBucket[j]) * ts.decay) //TODO overflow
	}
}
