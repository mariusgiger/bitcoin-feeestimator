package utils

// These are the multipliers for bitcoin denominations.
// Example: To get the satoshi value of an amount in 'btc', use
//
//    new(big.Int).Mul(value, big.NewInt(params.BTC))
//
const (
	Satoshi = 1
	BTC     = 1e8
)

//MaxFeeRate defines an upper bound for fees (satoshi per byte)
var MaxFeeRate = 500
