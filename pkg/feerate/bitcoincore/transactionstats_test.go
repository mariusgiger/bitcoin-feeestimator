package bitcoincore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TransactionStatsTestSuite struct {
	suite.Suite
	buckets       []int
	BLOCK_PERIODS int
	SCALE         int
	DECAY         float64
}

func (suite *TransactionStatsTestSuite) SetupSuite() {
	suite.buckets = make([]int, 3) // Fees in satoshis. Transactions will be grouped by that values
	suite.buckets[0] = 1000
	suite.buckets[1] = 2000
	suite.buckets[2] = 3000

	suite.BLOCK_PERIODS = 4 // How long transactions tracked
	suite.SCALE = 1
	suite.DECAY = 0.8 // How fast old transactions stats decaying. 0.8 will halve data importance in 3 blocks
}

func (suite *TransactionStatsTestSuite) TestShouldSaveDataInCorrectBuckets() {
	// arrange
	stats := NewTransactionStats(suite.buckets, suite.BLOCK_PERIODS, suite.DECAY, suite.SCALE)

	// act & assert Match bucket at index 2
	stats.record(1, 3500)
	assert.Equal(suite.T(), float64(3500), stats.feeSumPerBucket[2])
	assert.Equal(suite.T(), 1, stats.confirmedTransactionsPerBucket[2])

	stats.record(1, 4000)
	assert.Equal(suite.T(), float64(3500+4000), stats.feeSumPerBucket[2])
	assert.Equal(suite.T(), 2, stats.confirmedTransactionsPerBucket[2])

	//act & assert Match bucket at index 1
	stats.record(2, 2200) //TODO this was 2200 in original test which makes no sense depending on their logic
	assert.Equal(suite.T(), float64(2200), stats.feeSumPerBucket[1])
	assert.Equal(suite.T(), 1, stats.confirmedTransactionsPerBucket[1])

	//act & assert Match bucket at index 0
	stats.record(3, 1100)
	assert.Equal(suite.T(), float64(1100), stats.feeSumPerBucket[0])
	assert.Equal(suite.T(), 1, stats.confirmedTransactionsPerBucket[0])
}

func (suite *TransactionStatsTestSuite) TestShouldDecayAllStatsByDecayCoefPassedToConstructor() {
	// arrange
	stats := NewTransactionStats(suite.buckets, suite.BLOCK_PERIODS, suite.DECAY, suite.SCALE)
	// act
	stats.record(1, 3500)
	stats.record(1, 4000)
	stats.record(2, 2200)
	stats.record(3, 1100)
	stats.updateMovingAverages()

	//assert
	assert.Equal(suite.T(), float64(3500+4000)*suite.DECAY, stats.feeSumPerBucket[2])
	assert.Equal(suite.T(), float64(2200)*suite.DECAY, stats.feeSumPerBucket[1])
	assert.Equal(suite.T(), float64(1100)*suite.DECAY, stats.feeSumPerBucket[0])

	assert.Equal(suite.T(), int(2*suite.DECAY), stats.confirmedTransactionsPerBucket[2])
	assert.Equal(suite.T(), int(1*suite.DECAY), stats.confirmedTransactionsPerBucket[1])
	assert.Equal(suite.T(), int(1*suite.DECAY), stats.confirmedTransactionsPerBucket[0])
}

func (suite *TransactionStatsTestSuite) TestShouldGiveCorrectEstimations() {
	// arrange
	stats := NewTransactionStats(suite.buckets, suite.BLOCK_PERIODS, suite.DECAY, suite.SCALE)
	// Require an avg of 0.1 tx in the combined feerate bucket per block to have stat significance
	confirmationsPerBlock := 0.1
	desiredSuccessProbability := 0.5
	lowestPossibleFeeRequired := true
	// This number can not be bigger than max transaction tracking period, i.g. stats.maxPeriods
	desiredConfirmationsCount := 4
	// Assume that current block height is 4.
	currentBlockHeight := 4
	stats.record(1, 3500)
	stats.record(1, 4000)
	stats.record(2, 2200)
	stats.record(2, 2200)
	stats.record(2, 2200)
	stats.record(2, 2200)
	stats.record(2, 2200)
	stats.record(2, 2200)
	stats.record(3, 1100)

	// act
	estimation, estimationStats := stats.estimateMedianVal(
		desiredConfirmationsCount,
		confirmationsPerBlock,
		desiredSuccessProbability,
		lowestPossibleFeeRequired,
		currentBlockHeight,
	)

	//assert
	assert.NotZero(suite.T(), estimation)
	assert.NotNil(suite.T(), estimationStats)
}

func TestTransactionStatsTestSuite(t *testing.T) {
	suite.Run(t, new(TransactionStatsTestSuite))
}
