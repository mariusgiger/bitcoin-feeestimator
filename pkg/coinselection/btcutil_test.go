package coinselection

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
)

func NewUTXO(value int64) *common.UTXO {
	utxo := &common.UTXO{
		Value: value,
	}
	return utxo
}

type coinSelectTest struct {
	selector      Strategy
	inputCoins    []*common.UTXO
	targetValue   int64
	expectedCoins []*common.UTXO
	expectedError error
}

func testCoinSelector(tests []coinSelectTest, t *testing.T) {
	for _, test := range tests {
		set, err := test.selector.SelectCoins(test.inputCoins, test.targetValue)
		if test.expectedError != nil {
			assert.Equal(t, test.expectedError, err)
			continue
		}

		if test.expectedCoins != nil {
			require.NotNil(t, set)
			assert.Equal(t, len(test.expectedCoins), len(set.Coins))
			for n := 0; n < len(test.expectedCoins); n++ {
				assert.Equal(t, test.expectedCoins[n].Value, set.Coins[n].Value)
			}
		}
	}
}

var coins = []*common.UTXO{
	NewUTXO(100000000),
	NewUTXO(10000000),
	NewUTXO(50000000),
	NewUTXO(25000000),
}

var minIndexSelectors = []MinIndexCoinSelector{
	{MaxInputs: 10, MinChangeAmount: 10000},
	{MaxInputs: 2, MinChangeAmount: 10000},
}

var minIndexTests = []coinSelectTest{
	{minIndexSelectors[0], coins, coins[0].Value - minIndexSelectors[0].MinChangeAmount, []*common.UTXO{coins[0]}, nil},
	{minIndexSelectors[0], coins, coins[0].Value - minIndexSelectors[0].MinChangeAmount + 1, []*common.UTXO{coins[0], coins[1]}, nil},
	{minIndexSelectors[0], coins, 100000000, []*common.UTXO{coins[0]}, nil},
	{minIndexSelectors[0], coins, 110000000, []*common.UTXO{coins[0], coins[1]}, nil},
	{minIndexSelectors[0], coins, 140000000, []*common.UTXO{coins[0], coins[1], coins[2]}, nil},
	{minIndexSelectors[0], coins, 200000000, nil, ErrCoinsNoSelectionAvailable},
	{minIndexSelectors[1], coins, 10000000, []*common.UTXO{coins[0]}, nil},
	{minIndexSelectors[1], coins, 110000000, []*common.UTXO{coins[0], coins[1]}, nil},
	{minIndexSelectors[1], coins, 140000000, nil, ErrCoinsNoSelectionAvailable},
}

func TestMinIndexSelector(t *testing.T) {
	testCoinSelector(minIndexTests, t)
}

var minNumberSelectors = []MinNumberCoinSelector{
	{MaxInputs: 10, MinChangeAmount: 10000},
	{MaxInputs: 2, MinChangeAmount: 10000},
}

var minNumberTests = []coinSelectTest{
	{minNumberSelectors[0], coins, coins[0].Value - minNumberSelectors[0].MinChangeAmount, []*common.UTXO{coins[0]}, nil},
	{minNumberSelectors[0], coins, coins[0].Value - minNumberSelectors[0].MinChangeAmount + 1, []*common.UTXO{coins[0], coins[2]}, nil},
	{minNumberSelectors[0], coins, 100000000, []*common.UTXO{coins[0]}, nil},
	{minNumberSelectors[0], coins, 110000000, []*common.UTXO{coins[0], coins[2]}, nil},
	{minNumberSelectors[0], coins, 160000000, []*common.UTXO{coins[0], coins[2], coins[3]}, nil},
	{minNumberSelectors[0], coins, 184990000, []*common.UTXO{coins[0], coins[2], coins[3], coins[1]}, nil},
	{minNumberSelectors[0], coins, 184990001, nil, ErrCoinsNoSelectionAvailable},
	{minNumberSelectors[0], coins, 200000000, nil, ErrCoinsNoSelectionAvailable},
	{minNumberSelectors[1], coins, 10000000, []*common.UTXO{coins[0]}, nil},
	{minNumberSelectors[1], coins, 110000000, []*common.UTXO{coins[0], coins[2]}, nil},
	{minNumberSelectors[1], coins, 140000000, []*common.UTXO{coins[0], coins[2]}, nil},
}

func TestMinNumberSelector(t *testing.T) {
	testCoinSelector(minNumberTests, t)
}
