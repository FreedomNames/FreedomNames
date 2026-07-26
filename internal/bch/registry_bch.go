package bch

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/registry"
)

// reverseBytesHex decodes a display-order hex hash and returns internal order.
func reverseBytesHex(h string) []byte {
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil
	}
	return reverseBytes(b)
}

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
	client  *ElectrumClient
	minConf int64

	mu    sync.Mutex
	cache map[string]ownerCacheEntry
}

type ownerCacheEntry struct {
	pubKey    []byte // nil for a negative (not-found) entry
	notFound  bool
	expiresAt time.Time
}

// resolver.Cache TTLs. Positive results are cached briefly for reorg tolerance; negative
// results are cached even more briefly, just enough to blunt a random-name
// lookup flood without delaying a legitimate new claim for long.
const (
	ownerCacheTTL    = 5 * time.Minute
	notFoundCacheTTL = 30 * time.Second
)

// maxCustodyHops caps the NFT custody-chain walk so a name whose NFT was moved
// through many hops cannot make one lookup do unbounded work.
const maxCustodyHops = 64

// maxHistoryScan caps how many transactions one lookup will fetch from a single
// address history. Anyone can pay dust to a name's marker script (or to the
// address currently holding a name NFT) as many times as they like, and every
// entry costs a round-trip to the electrum server. Without a cap, a name could
// be made arbitrarily expensive to resolve — and resolution is reachable from
// any unauthenticated .fn query. Which end of the history gets scanned depends
// on what the caller is looking for — see oldestHistory and newestHistory — and
// a scan that hits the cap without finding its answer reports
// errHistoryTruncated rather than a definitive "no".
const maxHistoryScan = 512

// maxOwnerCacheEntries bounds the owner cache. Entries are keyed by requested
// label, and lookups for names that do not exist are cached too, so an
// unbounded map is a memory-exhaustion target for anyone who can send this node
// a stream of random <random>.fn queries.
const maxOwnerCacheEntries = 4096

// NewBCHRegistry builds a registry over the given electrum client.
func NewBCHRegistry(client *ElectrumClient, minConf int64) *bchRegistry {
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
	label, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if e, ok := r.cache[label]; ok && time.Now().Before(e.expiresAt) {
		r.mu.Unlock()
		if e.notFound {
			return nil, registry.ErrRegistryNotFound
		}
		return e.pubKey, nil
	}
	r.mu.Unlock()

	// A background context bounded by our own timeout; ResolveOwner has no
	// caller ctx (the NameRegistry interface is intentionally minimal).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pubKey, err := r.resolve(ctx, label)
	if err != nil {
		// Negative-cache a definitive not-found to blunt random-name floods.
		// Transient failures (timeouts, server errors) are not cached.
		if errors.Is(err, registry.ErrRegistryNotFound) {
			r.cacheStore(label, ownerCacheEntry{notFound: true, expiresAt: time.Now().Add(notFoundCacheTTL)})
		}
		return nil, err
	}

	r.cacheStore(label, ownerCacheEntry{pubKey: pubKey, expiresAt: time.Now().Add(ownerCacheTTL)})
	return pubKey, nil
}

// cacheStore records an entry, keeping the cache bounded: expired entries are
// dropped first, and if that is not enough the soonest-expiring entry is
// evicted to make room.
func (r *bchRegistry) cacheStore(label string, entry ownerCacheEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.cache) >= maxOwnerCacheEntries {
		now := time.Now()
		for k, e := range r.cache {
			if now.After(e.expiresAt) {
				delete(r.cache, k)
			}
		}
	}
	for len(r.cache) >= maxOwnerCacheEntries {
		oldest, found := "", false
		for k, e := range r.cache {
			if !found || e.expiresAt.Before(r.cache[oldest].expiresAt) {
				oldest, found = k, true
			}
		}
		if !found {
			break
		}
		delete(r.cache, oldest)
	}
	r.cache[label] = entry
}

// CategoryFor returns the NFT category for a normalized label (the category of
// the earliest confirmed FN01 claim), or an error if the name is unclaimed.
func (r *bchRegistry) CategoryFor(ctx context.Context, label string) ([]byte, error) {
	_, category, err := r.winningClaim(ctx, label)
	return category, err
}

// winningClaim finds the authoritative FN01 claim for a name: the earliest
// confirmed one (deterministic tiebreak on smaller txid). It returns the parsed
// claim tx and its NFT category (the genesis input's prevout txid).
func (r *bchRegistry) winningClaim(ctx context.Context, label string) (*parsedTx, []byte, error) {
	tip, err := r.client.BlockHeight(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("chain tip: %w", err)
	}
	history, err := r.client.GetHistory(ctx, scriptHash(markerScript(label)))
	if err != nil {
		return nil, nil, err
	}

	scan, truncated := oldestHistory(history, label)
	var claimTx *parsedTx
	var claimTxID []byte
	var claimHeight int64 = -1
	for _, h := range scan {
		if !confirmed(h.Height, tip, r.minConf) {
			continue
		}
		raw, err := r.client.GetRawTransaction(ctx, h.TxHash)
		if err != nil {
			return nil, nil, err
		}
		tx, err := parseTx(raw)
		if err != nil {
			continue
		}
		if tag, _, ok := parseFNMetadata(tx, label); !ok || tag != fnClaimTag {
			continue
		}
		txid := reverseBytesHex(h.TxHash)
		if claimHeight == -1 || h.Height < claimHeight ||
			(h.Height == claimHeight && bytes.Compare(txid, claimTxID) < 0) {
			claimTx, claimTxID, claimHeight = tx, txid, h.Height
		}
	}
	if claimTx == nil {
		if truncated {
			return nil, nil, fmt.Errorf("claim for %q: %w", label, errHistoryTruncated)
		}
		return nil, nil, registry.ErrRegistryNotFound
	}
	category := genesisCategory(claimTx)
	if category == nil {
		return nil, nil, registry.ErrRegistryNotFound
	}
	return claimTx, category, nil
}

// resolve performs the full chain lookup for a normalized label.
func (r *bchRegistry) resolve(ctx context.Context, label string) ([]byte, error) {
	tip, err := r.client.BlockHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("chain tip: %w", err)
	}
	history, err := r.client.GetHistory(ctx, scriptHash(markerScript(label)))
	if err != nil {
		return nil, fmt.Errorf("marker history for %q: %w", label, err)
	}
	if len(history) == 0 {
		return nil, registry.ErrRegistryNotFound
	}

	// Collect every valid FN01/FN02 binding for this name, keyed by the
	// hash160 of its revealed pubkey. The map is the ONLY way a pubkey becomes
	// authoritative: it must match the live NFT commitment. There is no
	// height-based fallback, so a stranger who merely pays the marker dust and
	// posts an FN02 (without holding the NFT) can never hijack the name.
	bindings := make(map[string][]byte) // hex(hash160(pubkey)) -> pubkey
	var claimTx *parsedTx
	var claimTxID []byte
	var claimHeight int64 = -1

	// A truncated scan can miss an FN02 rebind, which is by nature at the recent
	// end. A binding that IS found stays trustworthy — it is only authoritative
	// once it matches the live NFT commitment below — but a miss cannot be
	// reported as "unclaimed".
	scan, truncated := oldestHistory(history, label)
	for _, h := range scan {
		if !confirmed(h.Height, tip, r.minConf) {
			continue
		}
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
		bindings[hex.EncodeToString(hash160(pubKey))] = pubKey

		if tag == fnClaimTag {
			txid := reverseBytesHex(h.TxHash)
			// Earliest confirmed claim wins; deterministic tiebreak on the
			// smaller txid so every resolver agrees even for same-block claims.
			if claimHeight == -1 || h.Height < claimHeight ||
				(h.Height == claimHeight && bytes.Compare(txid, claimTxID) < 0) {
				claimTx, claimTxID, claimHeight = tx, txid, h.Height
			}
		}
	}

	if claimTx == nil {
		if truncated {
			return nil, fmt.Errorf("claim for %q: %w", label, errHistoryTruncated)
		}
		return nil, registry.ErrRegistryNotFound
	}

	// The NFT category is the prevout txid of the claim's genesis input (the
	// input spending output index 0), per the CashTokens genesis rule.
	category := genesisCategory(claimTx)
	if category == nil {
		return nil, registry.ErrRegistryNotFound
	}

	// Walk the NFT from the claim's mint output to the current UTXO, reading the
	// live commitment.
	commitment, err := r.currentCommitment(ctx, claimTxID, category)
	if err != nil {
		return nil, err
	}

	// The owner is the revealed pubkey whose hash160 equals the live commitment.
	// If none matches (e.g. the NFT was moved by a plain wallet transfer and not
	// yet re-bound via `freedom adopt`), the name has no resolvable owner.
	if pub, ok := bindings[hex.EncodeToString(commitment)]; ok {
		return pub, nil
	}
	if truncated {
		// The owning binding may simply be in the part we never read. Reporting
		// "unclaimed" here would negative-cache a name that is fine, which is a
		// per-name denial of service anyone can trigger: the marker address is
		// derived from the label alone, so anyone can pad its history with dust.
		return nil, fmt.Errorf("owner binding for %q: %w", label, errHistoryTruncated)
	}
	return nil, registry.ErrRegistryNotFound
}

// genesisCategory returns the CashTokens category a claim tx mints: the prevout
// txid of its first input that spends output index 0.
func genesisCategory(tx *parsedTx) []byte {
	for _, in := range tx.Inputs {
		if in.PrevIndex == 0 {
			return append([]byte(nil), in.PrevTxID...)
		}
	}
	return nil
}

// currentCommitment follows the name NFT from the mint output of the claim
// transaction (claimTxID) to the current UTXO and returns its live commitment.
// category is the NFT's token category (used to identify the token at each hop).
func (r *bchRegistry) currentCommitment(ctx context.Context, claimTxID, category []byte) ([]byte, error) {
	raw, err := r.client.GetRawTransaction(ctx, hex.EncodeToString(reverseBytes(claimTxID)))
	if err != nil {
		return nil, fmt.Errorf("fetch claim tx: %w", err)
	}
	tx, err := parseTx(raw)
	if err != nil {
		return nil, err
	}

	// Find the mint output: the output carrying our category token.
	mintVout := -1
	for i, o := range tx.Outputs {
		if o.Token != nil && bytesEqual(o.Token.CategoryID, category) {
			mintVout = i
			break
		}
	}
	if mintVout < 0 {
		return nil, registry.ErrRegistryNotFound
	}

	curTxID := claimTxID
	curVout := uint32(mintVout)
	commitment := tx.Outputs[mintVout].Token.Commitment
	curScript := tx.Outputs[mintVout].Script

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
	scan, truncated := newestHistory(history, "custody hop")
	for _, h := range scan {
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
	if truncated {
		// found=false means "this outpoint is unspent", which stops the custody
		// walk and freezes the answer at the current holder. Saying that from a
		// partial scan would keep resolving a transferred name to its previous
		// owner, so fail the lookup instead.
		return nil, nil, false, fmt.Errorf("custody hop: %w", errHistoryTruncated)
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
		if _, err := record.UnmarshalOwnerPubKey(pushes[2]); err != nil {
			continue
		}
		return t, pushes[2], true
	}
	return "", nil, false
}

// --- small helpers ---

// errHistoryTruncated reports that a lookup hit maxHistoryScan and so could not
// reach a definitive answer. It deliberately does NOT wrap registry.ErrRegistryNotFound:
// a truncated scan says "I did not finish looking", not "the name is unclaimed",
// and only the latter may be negative-cached (see ResolveOwner).
var errHistoryTruncated = errors.New("address history too large to scan conclusively")

// oldestHistory keeps the earliest maxHistoryScan entries. Electrum returns
// confirmed history in ascending height order, and the earliest confirmed claim
// is the one that wins, so the oldest end is the half that decides a claim.
// The bool reports whether anything was dropped: a caller that finds nothing
// must not report a definitive answer from a partial scan.
func oldestHistory(history []electrumHistoryItem, what string) ([]electrumHistoryItem, bool) {
	if len(history) <= maxHistoryScan {
		return history, false
	}
	log.Printf("registry: %s has %d history entries, scanning the oldest %d", what, len(history), maxHistoryScan)
	return history[:maxHistoryScan], true
}

// newestHistory keeps the most recent maxHistoryScan entries, newest first. A
// transaction that spends an output is always mined at or after the one that
// created it, so a custody hop has to search from the recent end — taking the
// oldest entries here would scan precisely the half that cannot contain the
// spend.
func newestHistory(history []electrumHistoryItem, what string) ([]electrumHistoryItem, bool) {
	truncated := len(history) > maxHistoryScan
	if truncated {
		log.Printf("registry: %s has %d history entries, scanning the newest %d", what, len(history), maxHistoryScan)
		history = history[len(history)-maxHistoryScan:]
	}
	out := make([]electrumHistoryItem, len(history))
	for i, h := range history {
		out[len(history)-1-i] = h
	}
	return out, truncated
}

func confirmed(height, tip, minConf int64) bool {
	if height <= 0 {
		return false
	}
	return tip-height+1 >= minConf
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
