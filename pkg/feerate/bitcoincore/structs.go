package bitcoincore

type EstimatorBucket struct {
	Start          int
	End            int
	WithinTarget   int
	TotalConfirmed int
	InMempool      int
	LeftMempool    int
}

func NewEstimatorBucket(start int, end int, withinTarget int, totalConfirmed int, inMempool int, leftMempool int) *EstimatorBucket {
	return &EstimatorBucket{
		start,
		end,
		withinTarget,
		totalConfirmed,
		inMempool,
		leftMempool,
	}
}

type EstimationResult struct {
	Pass  *EstimatorBucket
	Fail  *EstimatorBucket
	Decay float64
	Scale int
}

func NewEstimationResult(passBucket *EstimatorBucket, failBucket *EstimatorBucket, decay float64, scale int) *EstimationResult {
	return &EstimationResult{passBucket, failBucket, decay, scale}
}

type FeeCalculation struct {
	Est            *EstimationResult
	DesiredTarget  int
	Reason         string
	ReturnedTarget int
}

type MempoolTransaction struct {
	BlockHeight int
	Height      int
	Hash        string
	Fee         float64
	Size        int
	BucketIndex int
}

func NewMempoolTransaction(transaction Transaction, bucketIndex int) MempoolTransaction {
	return MempoolTransaction{
		BlockHeight: transaction.height,
		Height:      transaction.height,
		Hash:        transaction.hash,
		Fee:         transaction.fee,
		Size:        transaction.size,
		BucketIndex: bucketIndex,
	}
}

type Transaction struct {
	id          string
	hash        string
	blockHeight int
	bucketIndex int
	height      int
	fee         float64
	size        int
}
