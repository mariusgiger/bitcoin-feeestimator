package core

import "log"

// TxConfirmStats used to track transactions that were included in a block. We will lump transactions into a bucket according to their
// approximate feerate and then track how long it took for those txs to be included in a block
// The tracking of unconfirmed (mempool) transactions is completely independent of the
// historical tracking of transactions that have been confirmed in a block.
type TxConfirmStats struct {
	//buckets we will group transactions into
	//The upper-bound of the range for the bucket (inclusive)
	buckets []float64

	//Map of bucket upper-bound to index into all vectors by bucket
	bucketMap map[float64]int

	// For each bucket X:
	// Count the total # of txs in each bucket
	// Track the historical moving average of this total over blocks
	txCtAvg []float64

	// Count the total # of txs confirmed within Y blocks in each bucket
	// Track the historical moving average of theses totals over blocks
	confAvg [][]float64

	// Track moving avg of txs which have been evicted from the mempool
	// after failing to be confirmed within Y blocks
	failAvg [][]float64

	// Sum the total feerate of all tx's in each bucket
	// Track the historical moving average of this total over blocks
	avg []float64

	// Combine the conf counts with tx counts to calculate the confirmation % for each Y,X
	// Combine the total value with the tx counts to calculate the avg feerate per bucket

	decay float64

	// Resolution (# of blocks) with which confirmations are tracked
	scale uint

	// Mempool counts of outstanding transactions
	// For each bucket X, track the number of transactions in the mempool
	// that are unconfirmed for each possible confirmation value Y
	unconfTxs [][]int

	// transactions still unconfirmed after GetMaxConfirms for each bucket
	oldUnconfTxs []int
}

func NewTxConfirmStats(defaultBuckets []float64, defaultBucketMap map[float64]int, maxPeriods int, decay float64, scale uint) *TxConfirmStats {
	if scale == 0 {
		panic("scale must be non-zero")
	}

	confAvg := make([][]float64, maxPeriods)
	for i := 0; i < maxPeriods; i++ {
		confAvg[i] = make([]float64, len(defaultBuckets))
	}

	failAvg := make([][]float64, maxPeriods)
	for i := 0; i < maxPeriods; i++ {
		failAvg[i] = make([]float64, len(defaultBuckets))
	}

	txCtAvg := make([]float64, len(defaultBuckets))
	avg := make([]float64, len(defaultBuckets))

	stats := &TxConfirmStats{
		decay:     decay,
		scale:     scale,
		confAvg:   confAvg,
		failAvg:   failAvg,
		txCtAvg:   txCtAvg,
		avg:       avg,
		buckets:   defaultBuckets,
		bucketMap: defaultBucketMap,
	}

	stats.ResizeInMemoryCounters(len(defaultBuckets))
	return stats
}

func (s *TxConfirmStats) ResizeInMemoryCounters(newBuckets int) {
	s.unconfTxs = make([][]int, s.GetMaxConfirms())
	for i := 0; i < len(s.unconfTxs); i++ {
		s.unconfTxs[i] = make([]int, newBuckets)
	}

	s.oldUnconfTxs = make([]int, newBuckets)
}

func (s *TxConfirmStats) GetMaxConfirms() uint {
	return s.scale * uint(len(s.confAvg))
}

// ClearCurrent roll the unconfirmed txs circular buffer
func (s *TxConfirmStats) ClearCurrent(nBlockHeight uint) {
	for i := 0; i < len(s.buckets); i++ {
		s.oldUnconfTxs[i] += s.unconfTxs[nBlockHeight%uint(len(s.unconfTxs))][i]
		s.unconfTxs[nBlockHeight%uint(len(s.unconfTxs))][i] = 0
	}
}

func (s *TxConfirmStats) Record(blocksToConfirm uint, val float64) {
	if blocksToConfirm < 1 { // blocksToConfirm is 1-based
		return
	}

	periodsToConfirm := (blocksToConfirm + s.scale - 1) / s.scale
	bucketindex := lowerBound(s.bucketMap, val)
	for i := int(periodsToConfirm); i <= len(s.confAvg); i++ {
		s.confAvg[i-1][bucketindex]++
	}
	s.txCtAvg[bucketindex]++
	s.avg[bucketindex] += val
}

func (s *TxConfirmStats) UpdateMovingAverages() {
	for j := 0; j < len(s.buckets); j++ {
		for i := 0; i < len(s.confAvg); i++ {
			s.confAvg[i][j] = s.confAvg[i][j] * s.decay
		}
		for i := 0; i < len(s.failAvg); i++ {
			s.failAvg[i][j] = s.failAvg[i][j] * s.decay
		}

		s.avg[j] = s.avg[j] * s.decay
		s.txCtAvg[j] = s.txCtAvg[j] * s.decay
	}
}

type EstimationResult struct {
	pass  *EstimatorBucket
	fail  *EstimatorBucket
	decay float64
	scale uint
}

type EstimatorBucket struct {
	start          float64
	end            float64
	withinTarget   float64
	totalConfirmed float64
	inMempool      float64
	leftMempool    float64
}

// returns a median of -1 on error conditions
func (s *TxConfirmStats) EstimateMedianVal(confTarget uint, sufficientTxVal float64, successBreakPoint float64, requireGreater bool, nBlockHeight uint) (*EstimationResult, float64) {
	// Counters for a bucket (or range of buckets)
	nConf := float64(0)    // Number of tx's confirmed within the confTarget
	totalNum := float64(0) // Total number of tx's that were ever confirmed
	extraNum := 0          // Number of tx's still in mempool for confTarget or longer
	failNum := float64(0)  // Number of tx's that were never confirmed but removed from the mempool after confTarget
	periodTarget := (confTarget + s.scale - 1) / s.scale

	maxbucketindex := len(s.buckets) - 1

	// requireGreater means we are looking for the lowest feerate such that all higher
	// values pass, so we start at maxbucketindex (highest feerate) and look at successively
	// smaller buckets until we reach failure.  Otherwise, we are looking for the highest
	// feerate such that all lower values fail, and we go in the opposite direction.
	startbucket := 0
	step := 1
	if requireGreater {
		startbucket = maxbucketindex
		step = -1
	}

	// We'll combine buckets until we have enough samples.
	// The near and far variables will define the range we've combined
	// The best variables are the last range we saw which still had a high
	// enough confirmation rate to count as success.
	// The cur variables are the current range we're counting.
	curNearBucket := startbucket
	bestNearBucket := startbucket
	curFarBucket := startbucket
	bestFarBucket := startbucket

	foundAnswer := false
	bins := uint(len(s.unconfTxs))
	newBucketRange := true
	passing := true
	passBucket := new(EstimatorBucket)
	failBucket := new(EstimatorBucket)

	// Start counting from highest(default) or lowest feerate transactions
	for bucket := startbucket; bucket >= 0 && bucket <= maxbucketindex; bucket += step {
		if newBucketRange {
			curNearBucket = bucket
			newBucketRange = false
		}
		curFarBucket = bucket
		nConf += s.confAvg[periodTarget-1][bucket]
		totalNum += s.txCtAvg[bucket]
		failNum += s.failAvg[periodTarget-1][bucket]
		for confct := confTarget; confct < s.GetMaxConfirms(); confct++ {
			extraNum += s.unconfTxs[(nBlockHeight-confct)%bins][bucket]
		}

		extraNum += s.oldUnconfTxs[bucket]
		// If we have enough transaction data points in this range of buckets,
		// we can test for success
		// (Only count the confirmed data points, so that each confirmation count
		// will be looking at the same amount of data and same bucket breaks)

		if totalNum >= sufficientTxVal/(1-s.decay) {
			curPct := nConf / (totalNum + failNum + float64(extraNum))

			// Check to see if we are no longer getting confirmed at the success rate
			if (requireGreater && curPct < successBreakPoint) || (!requireGreater && curPct > successBreakPoint) {
				if passing == true {
					// First time we hit a failure record the failed bucket
					failMinBucket := Min(curNearBucket, curFarBucket)
					failMaxBucket := Max(curNearBucket, curFarBucket)
					failBucket.start = 0
					if failMinBucket == 1 { //TODO does this reflect: failMinBucket ? buckets[failMinBucket - 1] : 0;
						failBucket.start = s.buckets[failMinBucket-1]
					}
					failBucket.end = s.buckets[failMaxBucket]
					failBucket.withinTarget = nConf
					failBucket.totalConfirmed = totalNum
					failBucket.inMempool = float64(extraNum)
					failBucket.leftMempool = failNum
					passing = false
				}
				continue
			} else {
				// Otherwise update the cumulative stats, and the bucket variables
				// and reset the counters

				failBucket = new(EstimatorBucket) // Reset any failed bucket, currently passing
				foundAnswer = true
				passing = true
				passBucket.withinTarget = nConf
				nConf = 0
				passBucket.totalConfirmed = totalNum
				totalNum = 0
				passBucket.inMempool = float64(extraNum)
				passBucket.leftMempool = failNum
				failNum = 0
				extraNum = 0
				bestNearBucket = curNearBucket
				bestFarBucket = curFarBucket
				newBucketRange = true
			}
		}
	}

	median := float64(-1)
	txSum := float64(0)

	// Calculate the "average" feerate of the best bucket range that met success conditions
	// Find the bucket with the median transaction and then report the average feerate from that bucket
	// This is a compromise between finding the median which we can't since we don't save all tx's
	// and reporting the average which is less accurate
	minBucket := Min(bestNearBucket, bestFarBucket)
	maxBucket := Max(bestNearBucket, bestFarBucket)
	for j := minBucket; j <= maxBucket; j++ {
		txSum += s.txCtAvg[j]
	}

	if foundAnswer && txSum != 0 {
		txSum = txSum / 2
		for j := minBucket; j <= maxBucket; j++ {
			if s.txCtAvg[j] < txSum {
				txSum -= s.txCtAvg[j]
			} else { // we're in the right bucket
				median = s.avg[j] / s.txCtAvg[j]
				break
			}
		}

		passBucket.start = 0
		if minBucket == 1 {
			passBucket.start = s.buckets[minBucket-1]
		}
		passBucket.end = s.buckets[maxBucket]
	}

	// If we were passing until we reached last few buckets with insufficient data, then report those as failed
	if passing && !newBucketRange {
		failMinBucket := Min(curNearBucket, curFarBucket)
		failMaxBucket := Max(curNearBucket, curFarBucket)
		failBucket.start = 0
		if failMinBucket == 1 {
			failBucket.start = s.buckets[failMinBucket-1]
		}
		failBucket.end = s.buckets[failMaxBucket]
		failBucket.withinTarget = nConf
		failBucket.totalConfirmed = totalNum
		failBucket.inMempool = float64(extraNum)
		failBucket.leftMempool = failNum
	}

	log.Printf("FeeEst: %v %v%.0f%% decay %.5f: feerate: %g from (%g - %g) %.2f%% %.1f/(%.1f %v mem %.1f out) Fail: (%g - %g) %.2f%% %.1f/(%.1f %f mem %.1f out)\n",
		confTarget, requireGreater, 100.0*successBreakPoint, s.decay, //requireGreater ? ">" : "<"
		median, passBucket.start, passBucket.end,
		100*passBucket.withinTarget/(passBucket.totalConfirmed+passBucket.inMempool+passBucket.leftMempool),
		passBucket.withinTarget, passBucket.totalConfirmed, passBucket.inMempool, passBucket.leftMempool,
		failBucket.start, failBucket.end,
		100*failBucket.withinTarget/(failBucket.totalConfirmed+failBucket.inMempool+failBucket.leftMempool),
		failBucket.withinTarget, failBucket.totalConfirmed, failBucket.inMempool, failBucket.leftMempool)

	result := &EstimationResult{
		pass:  passBucket,
		fail:  failBucket,
		decay: s.decay,
		scale: s.scale,
	}

	return result, median
}

func (s *TxConfirmStats) NewTx(nBlockHeight uint, val float64) int {
	bucketindex := lowerBound(s.bucketMap, val)
	blockIndex := nBlockHeight % uint(len(s.unconfTxs))
	s.unconfTxs[blockIndex][bucketindex]++
	return bucketindex
}

func (s *TxConfirmStats) RemoveTx(entryHeight uint, nBestSeenHeight uint, bucketindex int, inBlock bool) {
	//nBestSeenHeight is not updated yet for the new block
	blocksAgo := nBestSeenHeight - entryHeight
	if nBestSeenHeight == 0 { // the BlockPolicyEstimator hasn't seen any blocks yet
		blocksAgo = 0
	}

	if blocksAgo < 0 {
		log.Print("Blockpolicy error, blocks ago is negative for mempool tx\n")
		return //This can't happen because we call this with our best seen height, no entries can have higher
	}

	if blocksAgo >= uint(len(s.unconfTxs)) {
		if s.oldUnconfTxs[bucketindex] > 0 {
			s.oldUnconfTxs[bucketindex]--
		} else {
			log.Printf("Blockpolicy error, mempool tx removed from >25 blocks,bucketIndex=%v already\n", bucketindex)
		}
	} else {
		blockIndex := entryHeight % uint(len(s.unconfTxs))
		if s.unconfTxs[blockIndex][bucketindex] > 0 {
			s.unconfTxs[blockIndex][bucketindex]--
		} else {
			log.Printf("Blockpolicy error, mempool tx removed from blockIndex=%v,bucketIndex=%v already\n", blockIndex, bucketindex)
		}
	}

	if !inBlock && uint(blocksAgo) >= s.scale { // Only counts as a failure if not confirmed for entire period
		if s.scale != 0 {
			panic("scale is not 0")
		}
		periodsAgo := uint(blocksAgo) / s.scale
		for i := 0; uint(i) < periodsAgo && i < len(s.failAvg); i++ {
			s.failAvg[i][bucketindex]++
		}
	}
}
