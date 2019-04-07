package coinselection

import (
	"errors"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
)

type ByAmount []*common.UTXO

func (a ByAmount) Len() int           { return len(a) }
func (a ByAmount) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAmount) Less(i, j int) bool { return a[i].Value < a[j].Value }

// ResultSet represents a coin selection result
type ResultSet struct {
	Coins  []*common.UTXO
	Fee    int64
	Change int64
}

var (
	// ErrInsufficientFunds is returned if there are not enough coins
	ErrInsufficientFunds = errors.New("not enough coins")

	// ErrCoinsNoSelectionAvailable is returned when a CoinSelector believes there is no
	// possible combination of coins which can meet the requirements provided to the selector.
	ErrCoinsNoSelectionAvailable = errors.New("no coin selection possible")
)

// Strategy interface for coin selection
type Strategy interface {
	SelectCoins(utxos []*common.UTXO, target int64, feeRate int64) (*ResultSet, error)
}

// SatisfiesTargetValue checks that the totalValue is either exactly the targetValue
// or is greater than the targetValue by at least the minChange amount.
func SatisfiesTargetValue(targetValue int64, minChange int64, utxos []*common.UTXO) bool {
	totalValue := int64(0)
	for _, utxo := range utxos {
		totalValue += utxo.Value
	}

	return (totalValue == targetValue || totalValue >= targetValue+minChange)
}

// Assuming Pay-to-Public-Key-Hash
const (
	BytesTransactionOverhead = 10
	BytesPerOutput           = 34
	BytesPerInput            = 148
)

// MinimalFeeWithChange returns the minimal fee for a utxo set assuming P2PKH as well as a change output
func MinimalFeeWithChange(utxos []*common.UTXO, feePerKB int64) int64 {
	fee := int64(0)
	txSize := BytesTransactionOverhead + len(utxos)*BytesPerInput + 2*BytesPerOutput
	fee += int64(txSize) * feePerKB / 1000

	return fee
}
