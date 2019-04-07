package blockchain

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
	"github.com/pkg/errors"
	"github.com/mariusgiger/bitcoin-feeestimator/pkg/utils"
	"github.com/ybbus/jsonrpc"
)

// ElectrumX implements JSON RPC protocol over encrypted TCP connection.
type ElectrumX struct {
	hostname string // host
	address  string // host:port
}

// NOTE, there is no context used in RPC client,
// need to take care of deadline timeout

// NewElectrumX creates new ElectrumX client
func NewElectrumX(targetURL string) (jsonrpc.RPCClient, error) {
	// parse URL
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse URL")
	}

	// on "tls" scheme just use implementation below!
	if u.Scheme == "tls" {
		return &ElectrumX{
			hostname: u.Hostname(),
			address:  u.Host,
		}, nil // OK
	}

	// fallback to JSON RPC over HTTP
	return jsonrpc.NewClient(targetURL), nil
}

// Call do JSON RPC call (helper)
func (x *ElectrumX) Call(method string, params ...interface{}) (*jsonrpc.RPCResponse, error) {
	return x.CallRaw(jsonrpc.NewRequest(method, params...))
}

// CallRaw do JSON RPC call
// NOTE: not effective implementation - new connection per each RPC call!
// usually establishing a TLS connection is quite long process!
func (x *ElectrumX) CallRaw(request *jsonrpc.RPCRequest) (*jsonrpc.RPCResponse, error) {
	// TLS configuration, need to specify server name
	tlsCfg := &tls.Config{
		ServerName: x.hostname,
	}

	// establish TLS connection
	conn, err := tls.Dial("tcp", x.address, tlsCfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial")
	}
	defer utils.IgnoreErrorOn(conn.Close)

	// encode request
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode request")
	}

	// write request
	encoder := bufio.NewWriter(conn)
	_, err = encoder.Write(requestBytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to write request")
	}
	err = encoder.WriteByte('\n') // new line
	if err != nil {
		return nil, errors.Wrap(err, "failed to write request EOL")
	}
	err = encoder.Flush()
	if err != nil {
		return nil, errors.Wrap(err, "failed to flush request")
	}

	// read response
	decoder := bufio.NewReader(conn)
	responseBytes, err := decoder.ReadBytes('\n')
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response")
	}

	// decode response
	var response jsonrpc.RPCResponse
	err = json.Unmarshal(responseBytes, &response)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode response")
	}

	return &response, nil // OK
}

// CallFor helper method to call method and extract response
func (x *ElectrumX) CallFor(out interface{}, method string, params ...interface{}) error {
	resp, err := x.Call(method, params...)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return resp.Error
	}

	return resp.GetObject(out)
}

// CallBatch - not implemented yet
func (x *ElectrumX) CallBatch(requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	return x.CallBatchRaw(requests)
}

// CallBatchRaw - not implemented yet
func (x *ElectrumX) CallBatchRaw(requests jsonrpc.RPCRequests) (jsonrpc.RPCResponses, error) {
	return nil, fmt.Errorf("batch calls not implemented")
}

// default BTC network
var btcDefaultNet = &chaincfg.MainNetParams

// createElectrumXScriptHash
// https://electrumx.readthedocs.io/en/latest/protocol-basics.html#script-hashes
func createElectrumXScriptHash(address string) (string, error) {
	// decode address
	decodedAddress, err := btcutil.DecodeAddress(address, btcDefaultNet)
	if err != nil {
		return "", errors.Wrap(err, "failed to decode address")
	}

	// Create public key script
	script, err := txscript.PayToAddrScript(decodedAddress)
	if err != nil {
		return "", errors.Wrap(err, "failed to create script")
	}

	// Apply SHA256
	hash := sha256.Sum256(script)

	// Reverse order
	// https://github.com/golang/go/wiki/SliceTricks#reversing
	for i := len(hash)/2 - 1; i >= 0; i-- {
		k := len(hash) - 1 - i
		hash[i], hash[k] = hash[k], hash[i]
	}

	return hex.EncodeToString(hash[:]), nil // OK
}

// basicAuth converts username and password to base64-encoded string
// that can be used in `Authorization` header with `Basic` prefix
// see https://golang.org/pkg/net/http/#Request.SetBasicAuth
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
