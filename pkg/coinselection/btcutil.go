package coinselection

import (
	"sort"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
)

// MinIndexCoinSelector is a CoinSelector that attempts to construct a
// selection of coins whose total value is at least targetValue and prefers
// any number of lower indexes (as in the ordered array) over higher ones.
type MinIndexCoinSelector struct {
	MaxInputs       int
	MinChangeAmount int64
}

// SelectCoins will attempt to select coins using the algorithm described
// in the MinIndexCoinSelector struct.
func (s MinIndexCoinSelector) SelectCoins(utxos []*common.UTXO, target int64, feeRate int64) (*ResultSet, error) {
	set := &ResultSet{}
	for n := 0; n < len(utxos) && n < s.MaxInputs; n++ {
		set.Coins = append(set.Coins, utxos[n])
		if SatisfiesTargetValue(target, s.MinChangeAmount, set.Coins) {
			return set, nil
		}
	}
	return nil, ErrCoinsNoSelectionAvailable
}

// MinNumberCoinSelector is a CoinSelector that attempts to construct
// a selection of coins whose total value is at least targetValue
// that uses as few of the inputs as possible.
type MinNumberCoinSelector struct {
	MaxInputs       int
	MinChangeAmount int64
}

// SelectCoins will attempt to select coins using the algorithm described
// in the MinNumberCoinSelector struct.
func (s MinNumberCoinSelector) SelectCoins(utxos []*common.UTXO, target int64, feeRate int64) (*ResultSet, error) {
	sortedCoins := make([]*common.UTXO, 0, len(utxos))
	sortedCoins = append(sortedCoins, utxos...)
	sort.Sort(sort.Reverse(ByAmount(sortedCoins)))

	return MinIndexCoinSelector(s).SelectCoins(sortedCoins, target, feeRate)
}
