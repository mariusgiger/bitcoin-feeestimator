package blockchain

import (
	"math/big"

	"github.com/pkg/errors"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/common"
	"github.com/ybbus/jsonrpc"
)

type UTXOManager interface {
	GetUTXOs(address string) ([]*common.UTXO, error)
}

type ElectrumxUTXOManager struct {
	electrumX jsonrpc.RPCClient
	btcClient jsonrpc.RPCClient
}

// NewElectrumxUTXOManager creates new NewUTXOManager instance
func NewElectrumxUTXOManager() (UTXOManager, error) {
	// create ElectrumX JSON RPC client
	electrumX, err := NewElectrumX("") //opts.GetElectrumXURL()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ElectrumX RPC")
	}

	return &ElectrumxUTXOManager{
		electrumX: electrumX,
	}, nil // OK
}

// GetUTXOs gets all UTXOs of a given address
func (s *ElectrumxUTXOManager) GetUTXOs(address string) ([]*common.UTXO, error) {
	scriptHash, err := createElectrumXScriptHash(address)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ElectrumX script hash")
	}

	// ElectrumX response
	type UTXO struct {
		TxPos  *big.Int `json:"tx_pos"`
		Value  *big.Int `json:"value"` // satoshis
		TxHash string   `json:"tx_hash"`
		Height *big.Int `json:"height"`
	}

	// JSON RPC request
	var eutxos []UTXO
	err = s.electrumX.CallFor(&eutxos, "blockchain.scripthash.listunspent", scriptHash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get UTXOs from ElectrumX")
	}

	// copy UTXOs
	utxos := make([]*common.UTXO, 0, len(eutxos))
	for _, u := range eutxos {
		utxos = append(utxos,
			&common.UTXO{
				Index:  u.TxPos,
				Value:  u.Value.Int64(),
				Hash:   u.TxHash,
				Height: u.Height.Int64(),
			})
	}

	return utxos, nil // OK
}
