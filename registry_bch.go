package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// bchRegistry resolves bare names ("mysite.fn") to their controlling owner's
// Ed25519 public key by reading the Bitcoin Cash chain via an Electrum server.
//
// A name is a mutable CashTokens NFT. The binding between the human name and
// the NFT (and the owner's Ed25519 pubkey) is anchored by FN01/FN02 OP_RETURN
// metadata, and discovered through a per-name marker script. Resolution:
//  1. get_history on the name's marker script -> candidate claim/rebind txs,
//  2. earliest confirmed valid FN01 wins -> the NFT category,
//  3. walk the NFT's custody chain to the current UTXO and read its
//     commitment,
//  4. the owner pubkey is the latest revealed pubkey whose hash160 matches the
//     current commitment (falling back to the last valid binding if a plain
//     wallet transfer left the commitment unbound).
type bchRegistry struct {
	client  *electrumClient
	minConf int64

	mu    sync.Mutex
	cache map[string]ownerCacheEntry
}

type ownerCacheEntry struct {
	pubKey    []byte
	expiresAt time.Time
}

// ownerCacheTTL bounds how long a resolved owner is cached (reorg tolerance).
const ownerCacheTTL = 5 * time.Minute

// maxCustodyHops caps the NFT custody-chain walk to avoid unbounded work on a
// pathological (or malicious) history.
const maxCustodyHops = 200

// NewBCHRegistry builds a registry over the given electrum client.
func NewBCHRegistry(client *electrumClient, minConf int64) *bchRegistry {
	if minConf < 1 {
		minConf = 1
	}
	return &bchRegistry{
		client:  client,
		minConf: minConf,
		cache:   make(map[string]ownerCacheEntry),
	}
}

// ResolveOwner implements NameRegistry.
func (r *bchRegistry) ResolveOwner(name string) ([]byte, error) {
	label, err := normalizeRegistryName(name)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if e, ok := r.cache[label]; ok && time.Now().Before(e.expiresAt) {
		r.mu.Unlock()
		return e.pubKey, nil
	}
	r.mu.Unlock()

	// A background context bounded by our own timeout; ResolveOwner has no
	// caller ctx (the NameRegistry interface is intentionally minimal).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pubKey, err := r.resolve(ctx, label)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[label] = ownerCacheEntry{pubKey: pubKey, expiresAt: time.Now().Add(ownerCacheTTL)}
	r.mu.Unlock()
	return pubKey, nil
}

// categoryFor returns the NFT category (== earliest confirmed FN01 claim txid,
// internal order) for a normalized label, or an error if the name is unclaimed.
func (r *bchRegistry) categoryFor(ctx context.Context, label string) ([]byte, error) {
	history, err := r.client.GetHistory(ctx, scriptHash(markerScript(label)))
	if err != nil {
		return nil, err
	}
	tip := maxHeight(history)
	var category []byte
	var bestHeight int64
	for _, h := range history {
		if !confirmed(h.Height, tip, r.minConf) {
			continue
		}
		raw, err := r.client.GetRawTransaction(ctx, h.TxHash)
		if err != nil {
			return nil, err
		}
		tx, err := parseTx(raw)
		if err != nil {
			continue
		}
		if tag, _, ok := parseFNMetadata(tx, label); !ok || tag != fnClaimTag {
			continue
		}
		if category == nil || h.Height < bestHeight {
			txid, err := hex.DecodeString(h.TxHash)
			if err != nil {
				continue
			}
			category = reverseBytes(txid)
			bestHeight = h.Height
		}
	}
	if category == nil {
		return nil, ErrRegistryNotFound
	}
	return category, nil
}

// binding is a name->pubkey binding revealed by an FN01/FN02 OP_RETURN.
type binding struct {
	pubKey []byte // marshaled Ed25519 pubkey
	height int64
	tag    string
}

// resolve performs the full chain lookup for a normalized label.
func (r *bchRegistry) resolve(ctx context.Context, label string) ([]byte, error) {
	history, err := r.client.GetHistory(ctx, scriptHash(markerScript(label)))
	if err != nil {
		return nil, fmt.Errorf("marker history for %q: %w", label, err)
	}
	if len(history) == 0 {
		return nil, ErrRegistryNotFound
	}

	tipHeight := maxHeight(history)

	// Collect all valid FN01/FN02 bindings for this name, in block order.
	var claim *binding // earliest confirmed FN01
	var claimTxID []byte
	bindings := make(map[string]*binding) // by hex(pubkey hash160) for commitment matching
	var ordered []*binding

	for _, h := range history {
		if !confirmed(h.Height, tipHeight, r.minConf) {
			continue // ignore unconfirmed for authority
		}
		txid, err := hex.DecodeString(h.TxHash)
		if err != nil {
			continue
		}
		txid = reverseBytes(txid) // internal order
		raw, err := r.client.GetRawTransaction(ctx, h.TxHash)
		if err != nil {
			return nil, fmt.Errorf("fetch tx %s: %w", h.TxHash, err)
		}
		tx, err := parseTx(raw)
		if err != nil {
			continue
		}
		tag, pubKey, ok := parseFNMetadata(tx, label)
		if !ok {
			continue
		}
		b := &binding{pubKey: pubKey, height: h.Height, tag: tag}
		bindings[hex.EncodeToString(hash160(pubKey))] = b
		ordered = append(ordered, b)

		if tag == fnClaimTag && (claim == nil || h.Height < claim.height) {
			claim = b
			claimTxID = txid
		}
	}

	if claim == nil {
		return nil, ErrRegistryNotFound
	}

	// Walk the NFT custody chain from the genesis (claim) tx to the current
	// UTXO and read its commitment. The category is the claim tx's id.
	category := claimTxID
	commitment, err := r.currentCommitment(ctx, category)
	if err != nil {
		// If the custody walk fails (e.g. NFT not found), fall back to the
		// claim's own binding — the name is at least claimed.
		if errors.Is(err, ErrRegistryNotFound) {
			return claim.pubKey, nil
		}
		return nil, err
	}

	// The owner is the revealed pubkey whose hash160 matches the live
	// commitment. If none matches (plain wallet transfer, not yet adopted),
	// fall back to the latest valid binding.
	if b, ok := bindings[hex.EncodeToString(commitment)]; ok {
		return b.pubKey, nil
	}
	return latestBinding(ordered).pubKey, nil
}

// currentCommitment follows the name NFT (identified by category) from its
// genesis output to the current UTXO and returns its commitment.
func (r *bchRegistry) currentCommitment(ctx context.Context, category []byte) ([]byte, error) {
	// The genesis output is vout 0 of the claim tx (the mint output).
	raw, err := r.client.GetRawTransaction(ctx, hex.EncodeToString(reverseBytes(category)))
	if err != nil {
		return nil, fmt.Errorf("fetch genesis tx: %w", err)
	}
	tx, err := parseTx(raw)
	if err != nil {
		return nil, err
	}
	if len(tx.Outputs) == 0 || tx.Outputs[0].Token == nil {
		return nil, ErrRegistryNotFound
	}

	curTxID := category
	curVout := uint32(0)
	commitment := tx.Outputs[0].Token.Commitment
	curScript := tx.Outputs[0].Script

	for hop := 0; hop < maxCustodyHops; hop++ {
		// Is (curTxID, curVout) still unspent at the holder address? If so, we
		// are done. Otherwise find the tx that spent it and follow the token.
		spendTx, spendRaw, found, err := r.findSpender(ctx, curScript, curTxID, curVout)
		if err != nil {
			return nil, err
		}
		if !found {
			return commitment, nil // current UTXO reached
		}
		// Locate the output in spendTx that carries our category token.
		nextVout := -1
		for i, o := range spendTx.Outputs {
			if o.Token != nil && bytesEqual(o.Token.CategoryID, category) {
				nextVout = i
				commitment = o.Token.Commitment
				curScript = o.Script
				break
			}
		}
		if nextVout < 0 {
			// Token was burned; last known commitment stands.
			return commitment, nil
		}
		curTxID = txID(spendRaw)
		curVout = uint32(nextVout)
	}
	return commitment, nil
}

// findSpender looks for the transaction that spends outpoint (txid:vout) locked
// by script, by scanning that address's history. Returns found=false if the
// outpoint is still unspent.
func (r *bchRegistry) findSpender(ctx context.Context, script, txid []byte, vout uint32) (*parsedTx, []byte, bool, error) {
	history, err := r.client.GetHistory(ctx, scriptHash(script))
	if err != nil {
		return nil, nil, false, err
	}
	for _, h := range history {
		raw, err := r.client.GetRawTransaction(ctx, h.TxHash)
		if err != nil {
			return nil, nil, false, err
		}
		tx, err := parseTx(raw)
		if err != nil {
			continue
		}
		for _, in := range tx.Inputs {
			if in.PrevIndex == vout && bytesEqual(in.PrevTxID, txid) {
				return tx, raw, true, nil
			}
		}
	}
	return nil, nil, false, nil
}

// parseFNMetadata extracts the FN tag and revealed pubkey from a tx's
// OP_RETURN, verifying the name matches. Returns ok=false if the tx has no
// valid FN metadata for this label.
func parseFNMetadata(tx *parsedTx, label string) (tag string, pubKey []byte, ok bool) {
	for _, o := range tx.Outputs {
		pushes := parseOpReturn(o.Script)
		if len(pushes) < 3 {
			continue
		}
		t := string(pushes[0])
		if t != fnClaimTag && t != fnRebindTag {
			continue
		}
		if string(pushes[1]) != label {
			continue
		}
		// pushes[2] is the marshaled Ed25519 pubkey; sanity-check it parses.
		if _, err := unmarshalOwnerPubKey(pushes[2]); err != nil {
			continue
		}
		return t, pushes[2], true
	}
	return "", nil, false
}

// --- small helpers ---

func confirmed(height, tip, minConf int64) bool {
	if height <= 0 {
		return false
	}
	return tip-height+1 >= minConf
}

func maxHeight(history []electrumHistoryItem) int64 {
	var m int64
	for _, h := range history {
		if h.Height > m {
			m = h.Height
		}
	}
	return m
}

func latestBinding(bs []*binding) *binding {
	best := bs[0]
	for _, b := range bs {
		if b.height >= best.height {
			best = b
		}
	}
	return best
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
