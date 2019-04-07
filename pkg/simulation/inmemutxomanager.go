package simulation

import "github.com/mariusgiger/bitcoin-feeestimator/pkg/common"

type InMemoryUTXOManager struct {
	UTXOs map[int]common.UTXO
}

func NewInMemoryUTXOManager() InMemoryUTXOManager {
	return InMemoryUTXOManager{
		UTXOs: make(map[int]common.UTXO),
	}
}

// AddUTXO adds a utxo to the pool, idx is used as the identifier from the input list
func (m InMemoryUTXOManager) AddUTXO(value int64, idx int) {
	utxo := common.UTXO{
		Value: value,
		ID:    idx,
	}

	m.UTXOs[idx] = utxo
}

func (m InMemoryUTXOManager) GetUTXOs(address string) ([]*common.UTXO, error) {
	utxos := make([]*common.UTXO, 0)
	for _, utxo := range m.UTXOs {
		utxos = append(utxos, &utxo)
	}

	return utxos, nil
}

func (m InMemoryUTXOManager) RemoveUTXOs(utxos []*common.UTXO) {
	for _, utxo := range utxos {
		delete(m.UTXOs, utxo.ID)
	}
}
