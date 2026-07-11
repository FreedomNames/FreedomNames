package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// bchWallet is a minimal single-key BCH wallet used to fund and sign registry
// transactions (name-NFT mints and rebinds). It is intentionally simple: one
// secp256k1 key, one P2PKH address, coin selection over that address's UTXOs.
// It is NOT a general-purpose wallet — just enough to claim names.
type bchWallet struct {
	priv    *secp256k1.PrivateKey
	pkh     []byte // hash160 of the compressed pubkey
	network string // "mainnet" | "chipnet"
	client  *electrumClient
}

// bchKeyPath is where the wallet key is stored (separate from name keys).
func bchKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".freedom")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "bch.key"), nil
}

// loadOrCreateBCHWallet loads the wallet key from ~/.freedom/bch.key, creating
// one on first use (mode 0600, same pattern as the node identity key).
func loadOrCreateBCHWallet(network string, client *electrumClient) (*bchWallet, error) {
	path, err := bchKeyPath()
	if err != nil {
		return nil, err
	}

	var priv *secp256k1.PrivateKey
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("bch key %s has unexpected length %d", path, len(data))
		}
		priv = secp256k1.PrivKeyFromBytes(data)
	} else if os.IsNotExist(err) {
		priv, err = secp256k1.GeneratePrivateKeyFromRand(rand.Reader)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, priv.Serialize(), 0600); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	return &bchWallet{
		priv:    priv,
		pkh:     hash160(priv.PubKey().SerializeCompressed()),
		network: network,
		client:  client,
	}, nil
}

// script returns the wallet's P2PKH locking script.
func (w *bchWallet) script() []byte { return p2pkhScript(w.pkh) }

// Address returns the wallet's cashaddr string for display/funding.
func (w *bchWallet) Address() string {
	return encodeCashAddr(w.network, cashAddrP2PKH, w.pkh)
}

// walletUTXO is a spendable coin (optionally token-bearing).
type walletUTXO struct {
	txid   []byte // internal order
	index  uint32
	value  int64
	token  *tokenInfo
	height int64
}

// utxos fetches the wallet address's unspent outputs from the electrum server.
func (w *bchWallet) utxos(ctx context.Context) ([]walletUTXO, error) {
	entries, err := w.client.ListUnspent(ctx, scriptHash(w.script()))
	if err != nil {
		return nil, err
	}
	out := make([]walletUTXO, 0, len(entries))
	for _, e := range entries {
		u := walletUTXO{
			txid:   reverseBytes(mustDecodeHex(e.TxHash)),
			index:  e.TxPos,
			value:  e.Value,
			height: e.Height,
		}
		if e.TokenData != nil {
			u.token = electrumToTokenInfo(e.TokenData)
		}
		out = append(out, u)
	}
	return out, nil
}

// Balance returns the total spendable (non-token) satoshis.
func (w *bchWallet) Balance(ctx context.Context) (int64, error) {
	utxos, err := w.utxos(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, u := range utxos {
		if u.token == nil { // never count NFTs as spendable balance
			total += u.value
		}
	}
	return total, nil
}

// selectFunding picks plain (non-token) UTXOs covering `need` satoshis, largest
// first. It never selects token-bearing UTXOs, so a name NFT can never be
// accidentally spent as fee. `need` must already include the fee the caller
// budgeted; prefer selectToCover for fee-aware selection.
func selectFunding(utxos []walletUTXO, need int64) ([]walletUTXO, int64, error) {
	plain := make([]walletUTXO, 0, len(utxos))
	for _, u := range utxos {
		if u.token == nil {
			plain = append(plain, u)
		}
	}
	sort.Slice(plain, func(i, j int) bool { return plain[i].value > plain[j].value })

	var selected []walletUTXO
	var total int64
	for _, u := range plain {
		selected = append(selected, u)
		total += u.value
		if total >= need {
			return selected, total, nil
		}
	}
	return nil, 0, fmt.Errorf("insufficient funds: need %d sat, have %d", need, total)
}

// feePerByte is a simple flat fee rate (sat/byte). BCH fees are tiny; 1.1
// sat/byte comfortably clears the 1 sat/byte relay minimum.
const feePerByte = 2

// estimateFee approximates the fee for a tx with the given input/output counts.
// P2PKH input ~148 bytes, output ~34 bytes, plus overhead and a margin for the
// token prefix / OP_RETURN outputs.
func estimateFee(numIn, numOut int) int64 {
	size := 10 + numIn*148 + numOut*34 + 100 // +100 margin for token prefix + OP_RETURN
	return int64(size * feePerByte)
}

// electrumToTokenInfo converts electrum token_data into our internal tokenInfo,
// reversing the display-order category into internal order.
func electrumToTokenInfo(td *electrumTokenData) *tokenInfo {
	t := &tokenInfo{
		CategoryID: reverseBytes(mustDecodeHex(td.Category)),
	}
	if td.NFT != nil {
		t.Commitment = mustDecodeHex(td.NFT.Commitment)
		switch td.NFT.Capability {
		case "mutable":
			t.Capability = tokenCapabilityMutable
		case "minting":
			t.Capability = tokenCapabilityMinting
		default:
			t.Capability = tokenCapabilityNone
		}
	}
	return t
}

// mustDecodeHex decodes hex, returning nil on error (electrum values are
// server-provided hex; callers treat nil as "no data").
func mustDecodeHex(s string) []byte {
	b, err := decodeHexLenient(s)
	if err != nil {
		return nil
	}
	return b
}

var errNoWalletKey = errors.New("no BCH wallet key")

// buildClaimTx builds (and signs) an FN01 claim: it mints a mutable name NFT to
// the wallet's own address with commitment = hash160(ownerPub), attaches the
// FN01 OP_RETURN, and pays the discovery-marker dust output.
//
// Per the CashTokens genesis rule, the NFT's category id equals the prevout
// txid of an input that spends output index 0. selectFundingGenesis guarantees
// input 0 is such an outpoint, so the category is known up-front: it is
// inputs[0].PrevTxID. No serialization round-trip is needed.
func (w *bchWallet) buildClaimTx(ctx context.Context, label string, ownerPub []byte) ([]byte, error) {
	utxos, err := w.utxos(ctx)
	if err != nil {
		return nil, err
	}

	// Non-change outputs: the token mint (token dust) + OP_RETURN (0) + marker.
	outValue := int64(tokenDustLimit + dustLimit)
	selected, total, err := selectFundingGenesis(utxos, outValue, 4) // +1 output for change
	if err != nil {
		return nil, err
	}

	// The category is the genesis input's prevout txid (internal order).
	category := append([]byte(nil), selected[0].txid...)

	tx, privs := w.newTx(selected)
	tx.Outputs = []txOutput{
		{Value: tokenDustLimit, Script: w.script(), Token: &tokenInfo{
			CategoryID: category,
			Capability: tokenCapabilityMutable,
			Commitment: hash160(ownerPub),
		}},
		{Value: 0, Script: opReturnScript([]byte(fnClaimTag), []byte(label), ownerPub)},
		{Value: dustLimit, Script: markerScript(label)},
	}
	if change := total - outValue - estimateFee(len(selected), 4); change >= dustLimit {
		tx.Outputs = append(tx.Outputs, txOutput{Value: change, Script: w.script()})
	}
	return tx.Serialize(privs)
}

// buildRebindTx spends the held name NFT (an "adopt"/rebind), updating its
// commitment to hash160(newOwnerPub) and attaching FN02 metadata + marker.
func (w *bchWallet) buildRebindTx(ctx context.Context, label string, category, newOwnerPub []byte) ([]byte, error) {
	utxos, err := w.utxos(ctx)
	if err != nil {
		return nil, err
	}
	nftUTXO, ok := findNFT(utxos, category)
	if !ok {
		return nil, fmt.Errorf("this wallet does not hold the %q name NFT", label)
	}

	// The NFT input itself carries token dust; extra funding covers the marker
	// dust + fee (the token output re-uses the NFT's value).
	outValue := int64(dustLimit)
	funding, fundingTotal, err := selectFunding(utxos, outValue+estimateFee(3, 4))
	if err != nil {
		return nil, err
	}

	inputs := append([]walletUTXO{nftUTXO}, funding...)
	tx, privs := w.newTx(inputs)
	tx.Outputs = []txOutput{
		{Value: tokenDustLimit, Script: w.script(), Token: &tokenInfo{
			CategoryID: category,
			Capability: tokenCapabilityMutable,
			Commitment: hash160(newOwnerPub),
		}},
		{Value: 0, Script: opReturnScript([]byte(fnRebindTag), []byte(label), newOwnerPub)},
		{Value: dustLimit, Script: markerScript(label)},
	}
	// Total spendable = the NFT's own value + funding. Outputs consume the token
	// dust + marker dust; the rest (minus fee) returns as change.
	total := nftUTXO.value + fundingTotal
	if change := total - tokenDustLimit - dustLimit - estimateFee(len(inputs), 4); change >= dustLimit {
		tx.Outputs = append(tx.Outputs, txOutput{Value: change, Script: w.script()})
	}
	return tx.Serialize(privs)
}

// newTx builds an unsigned transaction skeleton over the selected UTXOs, all
// signed by the wallet key.
func (w *bchWallet) newTx(inputs []walletUTXO) (*transaction, []*secp256k1.PrivateKey) {
	tx := &transaction{Version: 2}
	privs := make([]*secp256k1.PrivateKey, len(inputs))
	for i, u := range inputs {
		tx.Inputs = append(tx.Inputs, txInput{
			PrevTxID:   u.txid,
			PrevIndex:  u.index,
			PrevScript: w.script(),
			PrevValue:  u.value,
			PrevToken:  u.token,
			Sequence:   0xffffffff,
		})
		privs[i] = w.priv
	}
	return tx, privs
}

// selectFundingGenesis selects plain UTXOs to cover outValue plus the mining
// fee, guaranteeing the first selected UTXO spends outpoint index 0 (the
// CashTokens genesis rule, so the tx can mint a new token category). numOutputs
// is the transaction's output count (used to size the fee). Fee is recomputed
// as inputs are added, so multi-input claims are never underpaid.
func selectFundingGenesis(utxos []walletUTXO, outValue int64, numOutputs int) ([]walletUTXO, int64, error) {
	var genesis *walletUTXO
	for i := range utxos {
		if utxos[i].token == nil && utxos[i].index == 0 {
			genesis = &utxos[i]
			break
		}
	}
	if genesis == nil {
		return nil, 0, errors.New("no eligible genesis coin (registering a name needs a plain coin whose output index is 0). Send a little BCH to your own wallet address and retry; the received coin is usually at index 0")
	}

	selected := []walletUTXO{*genesis}
	total := genesis.value
	need := func(n int) int64 { return outValue + estimateFee(n, numOutputs) }
	if total >= need(len(selected)) {
		return selected, total, nil
	}
	for i := range utxos {
		u := utxos[i]
		if u.token != nil || (bytesEqual(u.txid, genesis.txid) && u.index == genesis.index) {
			continue
		}
		selected = append(selected, u)
		total += u.value
		if total >= need(len(selected)) {
			return selected, total, nil
		}
	}
	return nil, 0, fmt.Errorf("insufficient funds: need ~%d sat, have %d", need(len(selected)), total)
}

// findNFT returns the wallet UTXO holding the NFT of the given category.
func findNFT(utxos []walletUTXO, category []byte) (walletUTXO, bool) {
	for _, u := range utxos {
		if u.token != nil && bytesEqual(u.token.CategoryID, category) {
			return u, true
		}
	}
	return walletUTXO{}, false
}
