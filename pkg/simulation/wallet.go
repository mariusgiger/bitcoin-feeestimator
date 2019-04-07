package simulation

import (
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/fees"
	"go.uber.org/zap"

	. "github.com/ahmetb/go-linq"
)

type Wallet struct {
	Address   string
	TxHistory []*Tx
	utxos     InMemoryUTXOManager
	estimator *fees.Estimator
	logger    *zap.Logger

	numberOfTxSent     int
	numberOfTxReceived int
	estimations        []*fees.EstimationResult
}

func (w *Wallet) Balance() int64 {
	sum := int64(0)
	utxos, _ := w.utxos.GetUTXOs(w.Address)
	for _, utxo := range utxos {
		sum = sum + utxo.Value
	}

	return sum
}

func (w *Wallet) NumberOfUTXOs() int {
	utxos, _ := w.utxos.GetUTXOs(w.Address)
	return len(utxos)
}

func (w *Wallet) ReceiveTx(tx *Tx, idx int) {
	w.numberOfTxReceived = w.numberOfTxReceived + 1
	w.utxos.AddUTXO(tx.Value, idx)
}

func (w *Wallet) SendTx(tx *Tx, idx int) error {
	w.numberOfTxSent = w.numberOfTxSent + 1
	estimation, err := w.estimator.EstimateFees(w.Address, tx.Value)
	if err != nil { //Handle insufficient funds
		return err
	}

	w.utxos.RemoveUTXOs(estimation.Set)
	w.estimations = append(w.estimations, estimation)
	return nil
}

func (w *Wallet) PrintStats() {
	avgFee := From(w.estimations).SelectT(func(e *fees.EstimationResult) int64 {
		return e.Fee
	}).Average()

	avgChange := From(w.estimations).SelectT(func(e *fees.EstimationResult) int64 {
		return e.Change
	}).Average()

	w.logger.Info("stats",
		zap.Any("number of tx sent", w.numberOfTxSent),
		zap.Any("number of tx received", w.numberOfTxReceived),
		zap.Any("avg fee", avgFee),
		zap.Any("avg change", avgChange),
		zap.Any("resulting balance", w.Balance()),
		zap.Any("resulting utxos", w.NumberOfUTXOs()),
	)
}
