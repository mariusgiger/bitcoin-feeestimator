package common

import "math/big"

// UTXO represents an unspent transaction output
type UTXO struct {
	Value  int64    `json:"value,omitempty"`
	Index  *big.Int `json:"index,omitempty"`
	Hash   string   `json:"hash,omitempty"`
	Height int64    `json:"height,omitempty"`
	ID     int
}
