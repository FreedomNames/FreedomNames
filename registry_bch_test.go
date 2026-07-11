package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"net"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/libp2p/go-libp2p/core/crypto"
)

// mockElectrum is an in-process Electrum server backed by a fixed set of raw
// transactions. It answers the exact methods the registry uses.
type mockElectrum struct {
	ln      net.Listener
	txByID  map[string][]byte                // txid hex (display) -> raw bytes
	history map[string][]electrumHistoryItem // scripthash -> history
	unspent map[string][]electrumUTXO        // scripthash -> utxos
}

func newMockElectrum(t *testing.T) *mockElectrum {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &mockElectrum{
		ln:      ln,
		txByID:  map[string][]byte{},
		history: map[string][]electrumHistoryItem{},
		unspent: map[string][]electrumUTXO{},
	}
	go m.serve()
	t.Cleanup(func() { ln.Close() })
	return m
}

// endpoint returns a tcp:// electrum endpoint for the mock.
func (m *mockElectrum) endpoint() string { return "tcp://" + m.ln.Addr().String() }

// addTx registers a raw tx and indexes it under the given scripts' histories.
func (m *mockElectrum) addTx(raw []byte, height int64, scripts ...[]byte) string {
	txidHex := hex.EncodeToString(reverseBytes(txID(raw)))
	m.txByID[txidHex] = raw
	for _, s := range scripts {
		sh := scriptHash(s)
		m.history[sh] = append(m.history[sh], electrumHistoryItem{Height: height, TxHash: txidHex})
	}
	return txidHex
}

func (m *mockElectrum) serve() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		go m.handle(conn)
	}
}

func (m *mockElectrum) handle(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var req struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		result := m.dispatch(req.Method, req.Params)
		resp, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
		conn.Write(append(resp, '\n'))
	}
}

func (m *mockElectrum) dispatch(method string, params []any) any {
	switch method {
	case "server.version":
		return []string{"mock", "1.5.3"}
	case "blockchain.scripthash.get_history":
		sh, _ := params[0].(string)
		if h, ok := m.history[sh]; ok {
			return h
		}
		return []electrumHistoryItem{}
	case "blockchain.scripthash.listunspent":
		sh, _ := params[0].(string)
		if u, ok := m.unspent[sh]; ok {
			return u
		}
		return []electrumUTXO{}
	case "blockchain.transaction.get":
		id, _ := params[0].(string)
		if raw, ok := m.txByID[id]; ok {
			return hex.EncodeToString(raw)
		}
		return ""
	default:
		return nil
	}
}

// makeClaimTx builds a valid FN01 claim transaction that mints a mutable name
// NFT to holderScript and reveals ownerPub. The NFT category equals this tx's
// own id (genesis: input 0 spends vout 0 of some funding outpoint).
func makeClaimTx(t *testing.T, label string, ownerPub []byte, holderScript []byte, fundingKey *secp256k1.PrivateKey, fundingTxID []byte) []byte {
	t.Helper()
	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   fundingTxID,
			PrevIndex:  0, // vout 0 -> eligible genesis input
			PrevScript: p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed())),
			PrevValue:  100000,
			Sequence:   0xffffffff,
		}},
		Outputs: []txOutput{
			// vout 0: the name NFT (category filled in after we know our txid;
			// but category == our txid, so we set it below via a placeholder).
			{Value: dustLimit, Script: holderScript, Token: &tokenInfo{
				Capability: tokenCapabilityMutable,
				Commitment: hash160(ownerPub),
			}},
			// FN01 metadata.
			{Value: 0, Script: opReturnScript([]byte(fnClaimTag), []byte(label), ownerPub)},
			// dust marker.
			{Value: dustLimit, Script: markerScript(label)},
		},
	}
	// The category is our own txid. We must compute the txid, which depends on
	// the serialized (signed) tx, then set the token category to that id. Since
	// the token category is inside the signed data, we compute the id first
	// with a zero category, then set it — for the mock this is acceptable
	// because resolution derives the category from the claim txid, and the
	// custody walk reads whatever category the genesis output carries. To keep
	// them consistent, set category = txid of the signed tx.
	raw, err := tx.Serialize([]*secp256k1.PrivateKey{fundingKey})
	if err != nil {
		t.Fatalf("serialize claim: %v", err)
	}
	category := txID(raw)
	tx.Outputs[0].Token.CategoryID = category
	raw, err = tx.Serialize([]*secp256k1.PrivateKey{fundingKey})
	if err != nil {
		t.Fatalf("re-serialize claim: %v", err)
	}
	return raw
}

func ownerPubBytes(t *testing.T, priv crypto.PrivKey) []byte {
	t.Helper()
	b, err := crypto.MarshalPublicKey(priv.GetPublic())
	if err != nil {
		t.Fatalf("marshal owner pub: %v", err)
	}
	return b
}

// TestBCHRegistryResolvesClaim builds a claim tx in the mock chain and checks
// the registry returns the owner pubkey.
func TestBCHRegistryResolvesClaim(t *testing.T) {
	m := newMockElectrum(t)
	ownerKey := newTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)

	fundingKey, _ := secp256k1.GeneratePrivateKey()
	holderScript := p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed()))

	raw := makeClaimTx(t, "mysite", ownerPub, holderScript, fundingKey, mustHex(t, repeat("ab", 32)))
	// Index under the marker script (for discovery) and holder script (custody).
	m.addTx(raw, 100, markerScript("mysite"), holderScript)

	client := newElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	got, err := reg.ResolveOwner("mysite.fn")
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if !bytesEqual(got, ownerPub) {
		t.Fatalf("resolved wrong owner pubkey")
	}
}

// TestBCHRegistryUnclaimedNotFound checks an unknown name is not found.
func TestBCHRegistryUnclaimedNotFound(t *testing.T) {
	m := newMockElectrum(t)
	client := newElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	if _, err := reg.ResolveOwner("ghost.fn"); err == nil {
		t.Fatal("expected not-found for an unclaimed name")
	}
}

// TestBCHRegistryEarliestClaimWins checks that when two claims exist for the
// same name, the earlier-confirmed one is authoritative.
func TestBCHRegistryEarliestClaimWins(t *testing.T) {
	m := newMockElectrum(t)
	firstKey := newTestKey(t)
	secondKey := newTestKey(t)
	firstPub := ownerPubBytes(t, firstKey)
	secondPub := ownerPubBytes(t, secondKey)

	fk1, _ := secp256k1.GeneratePrivateKey()
	fk2, _ := secp256k1.GeneratePrivateKey()
	holder1 := p2pkhScript(hash160(fk1.PubKey().SerializeCompressed()))
	holder2 := p2pkhScript(hash160(fk2.PubKey().SerializeCompressed()))

	// First claim at height 100, squatter's claim at height 105.
	raw1 := makeClaimTx(t, "prize", firstPub, holder1, fk1, mustHex(t, repeat("11", 32)))
	raw2 := makeClaimTx(t, "prize", secondPub, holder2, fk2, mustHex(t, repeat("22", 32)))
	m.addTx(raw2, 105, markerScript("prize"), holder2)
	m.addTx(raw1, 100, markerScript("prize"), holder1)

	client := newElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	got, err := reg.ResolveOwner("prize.fn")
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if !bytesEqual(got, firstPub) {
		t.Fatal("expected the earliest claimant to win")
	}
}

// TestBCHRegistryIgnoresUnconfirmed checks an unconfirmed-only claim is not
// authoritative.
func TestBCHRegistryIgnoresUnconfirmed(t *testing.T) {
	m := newMockElectrum(t)
	ownerKey := newTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)
	fk, _ := secp256k1.GeneratePrivateKey()
	holder := p2pkhScript(hash160(fk.PubKey().SerializeCompressed()))

	raw := makeClaimTx(t, "pending", ownerPub, holder, fk, mustHex(t, repeat("33", 32)))
	m.addTx(raw, 0, markerScript("pending"), holder) // height 0 = unconfirmed

	client := newElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	if _, err := reg.ResolveOwner("pending.fn"); err == nil {
		t.Fatal("expected an unconfirmed-only claim to be not-found")
	}
}
