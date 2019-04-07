package fees

import (
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/blockchain"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/coinselection"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"
)

type Estimator struct {
	Feerater feerate.FeeRater
	Selector coinselection.Strategy
	UTXOs    blockchain.UTXOManager
}

type EstimationResult struct {
	Set     []*common.UTXO
	FeeRate int64
	Fee     int64
	Change  int64
}

func (e *Estimator) EstimateFees(address string, targetValue int64) (*EstimationResult, error) {
	//get utxos for address
	utxos, err := e.UTXOs.GetUTXOs(address)

	// predict satoshi per byte rate
	rate, err := e.Feerater.GetFeeRate()
	if err != nil {
		return nil, err
	}

	// select coins
	set, err := e.Selector.SelectCoins(utxos, targetValue, rate)
	if err != nil {
		return nil, err
	}

	return &EstimationResult{
		Set:     set.Coins,
		FeeRate: rate,
		Fee:     set.Fee,
		Change:  set.Change,
	}, nil
}
