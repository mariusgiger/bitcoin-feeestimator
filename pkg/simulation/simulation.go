package simulation

import (
	"bufio"
	"encoding/csv"
	"io"
	"os"
	"strconv"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/coinselection"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/fees"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
	"go.uber.org/zap"
)

type Simulation struct {
	wallet      *Wallet
	logger      *zap.Logger
	txs         []*Tx
	startingSet []*Tx
}

type Tx struct {
	Value int64
	UTXOs []*common.UTXO
}

// GetFeeRate implements feerate.GetFeeRate
func (s *Simulation) GetFeeRate() (int64, error) {
	return 0, nil
}

func NewSimulation(logger *zap.Logger) *Simulation {
	txs := readTxs("data/moneypot.csv")
	startingSet := readTxs("data/UTXO-post-LF.csv")
	//determine if initial utxo set is needed

	utxos := NewInMemoryUTXOManager()
	sim := &Simulation{
		txs:         txs,
		logger:      logger,
		startingSet: startingSet,
	}
	estimator := &fees.Estimator{
		Feerater: sim,
		Selector: coinselection.RandomCoinSelector{MaxInputs: 10, MinChangeAmount: 0},
		UTXOs:    utxos,
	}
	wallet := &Wallet{
		estimator: estimator,
		logger:    logger,
		utxos:     utxos,
	}
	sim.wallet = wallet
	return sim
}

func readTxs(file string) []*Tx {
	csvFile, _ := os.Open(file)
	reader := csv.NewReader(bufio.NewReader(csvFile))
	var txs []*Tx
	for {
		line, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}

		value, err := strconv.ParseInt(line[0], 10, 64)
		if err != nil {
			panic(err)
		}
		txs = append(txs, &Tx{
			Value: value,
		})
	}

	return txs
}

func (s *Simulation) Run() error {
	index := 0
	//Setup
	for _, utxo := range s.startingSet[0:100] {
		s.wallet.utxos.AddUTXO(utxo.Value, index)
		index = index + 1
	}

	//Run
	for _, tx := range s.txs[0:1000] {
		if tx.Value > 0 { //if tx is incoming add utxo to pool
			s.wallet.ReceiveTx(tx, index)
		} else { //if tx is outgoing estimate fees
			err := s.wallet.SendTx(tx, index)
			if err != nil {
				return err
			}
		}

		index = index + 1
	}

	//Stats
	s.wallet.PrintStats()

	return nil
}
