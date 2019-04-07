package coinselection

import (
	"math/rand"
	"time"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
)

type RandomCoinSelector struct {
	MaxInputs       int
	MinChangeAmount int64
}

func (s RandomCoinSelector) SelectCoins(utxos []*common.UTXO, target int64, feeRate int64) (*ResultSet, error) {
	shuffledUtxos := shuffle(utxos)

	return MinIndexCoinSelector(s).SelectCoins(shuffledUtxos, target, feeRate)
}

func shuffle(utxos []*common.UTXO) []*common.UTXO {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	res := make([]*common.UTXO, len(utxos))
	perm := r.Perm(len(utxos))
	for i, randIndex := range perm {
		res[i] = utxos[randIndex]
	}
	return res
}
