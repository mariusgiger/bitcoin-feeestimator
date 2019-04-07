package feerate

type FeeRater interface {
	//GetFeeRate returns the current fee rate in satoshi per kb
	GetFeeRate() (int64, error)
}
