package bch

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"net"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/libp2p/go-libp2p/core/crypto"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
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
	case "blockchain.headers.subscribe":
		// A tip far above any test height so claims count as confirmed.
		return map[string]any{"height": 1000000}
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
// NFT to holderScript and reveals ownerPub. Per the CashTokens genesis rule the
// NFT category is the prevout txid of input 0 (which spends output index 0),
// i.e. fundingTxID.
func makeClaimTx(t *testing.T, label string, ownerPub []byte, holderScript []byte, fundingKey *secp256k1.PrivateKey, fundingTxID []byte) []byte {
	t.Helper()
	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   fundingTxID,
			PrevIndex:  0, // vout 0 -> eligible genesis input; category = fundingTxID
			PrevScript: p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed())),
			PrevValue:  100000,
			Sequence:   0xffffffff,
		}},
		Outputs: []txOutput{
			// vout 0: the name NFT, category = the genesis input's prevout txid.
			{Value: tokenDustLimit, Script: holderScript, Token: &tokenInfo{
				CategoryID: fundingTxID,
				Capability: tokenCapabilityMutable,
				Commitment: hash160(ownerPub),
			}},
			// FN01 metadata.
			{Value: 0, Script: opReturnScript([]byte(fnClaimTag), []byte(label), ownerPub)},
			// dust marker.
			{Value: dustLimit, Script: markerScript(label)},
		},
	}
	raw, err := tx.Serialize([]*secp256k1.PrivateKey{fundingKey})
	if err != nil {
		t.Fatalf("serialize claim: %v", err)
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
	ownerKey := testsupport.NewTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)

	fundingKey, _ := secp256k1.GeneratePrivateKey()
	holderScript := p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed()))

	raw := makeClaimTx(t, "mysite", ownerPub, holderScript, fundingKey, mustHex(t, repeat("ab", 32)))
	// Index under the marker script (for discovery) and holder script (custody).
	m.addTx(raw, 100, markerScript("mysite"), holderScript)

	client := NewElectrumClient(m.endpoint())
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
	client := NewElectrumClient(m.endpoint())
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
	firstKey := testsupport.NewTestKey(t)
	secondKey := testsupport.NewTestKey(t)
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

	client := NewElectrumClient(m.endpoint())
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
	ownerKey := testsupport.NewTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)
	fk, _ := secp256k1.GeneratePrivateKey()
	holder := p2pkhScript(hash160(fk.PubKey().SerializeCompressed()))

	raw := makeClaimTx(t, "pending", ownerPub, holder, fk, mustHex(t, repeat("33", 32)))
	m.addTx(raw, 0, markerScript("pending"), holder) // height 0 = unconfirmed

	client := NewElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	if _, err := reg.ResolveOwner("pending.fn"); err == nil {
		t.Fatal("expected an unconfirmed-only claim to be not-found")
	}
}

// makeFakeRebind builds a tx that pays the marker dust and carries an FN02
// OP_RETURN for a name, but does NOT hold or spend the name NFT. This models an
// attacker trying to hijack resolution by publishing metadata alone.
func makeFakeRebind(t *testing.T, label string, attackerPub []byte, key *secp256k1.PrivateKey, fundingTxID []byte) []byte {
	t.Helper()
	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   fundingTxID,
			PrevIndex:  1,
			PrevScript: p2pkhScript(hash160(key.PubKey().SerializeCompressed())),
			PrevValue:  100000,
			Sequence:   0xffffffff,
		}},
		Outputs: []txOutput{
			{Value: 0, Script: opReturnScript([]byte(fnRebindTag), []byte(label), attackerPub)},
			{Value: dustLimit, Script: markerScript(label)},
		},
	}
	raw, err := tx.Serialize([]*secp256k1.PrivateKey{key})
	if err != nil {
		t.Fatalf("serialize fake rebind: %v", err)
	}
	return raw
}

// TestBCHRegistryRejectsMetadataHijack proves a stranger who pays the marker
// dust and posts an FN02 with their own pubkey, without holding the NFT, cannot
// steal name resolution. The owner is decided solely by the NFT commitment.
func TestBCHRegistryRejectsMetadataHijack(t *testing.T) {
	m := newMockElectrum(t)
	ownerKey := testsupport.NewTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)
	attackerKey := testsupport.NewTestKey(t)
	attackerPub := ownerPubBytes(t, attackerKey)

	fk, _ := secp256k1.GeneratePrivateKey()
	holder := p2pkhScript(hash160(fk.PubKey().SerializeCompressed()))
	ak, _ := secp256k1.GeneratePrivateKey()

	// Legitimate claim at height 100.
	claim := makeClaimTx(t, "target", ownerPub, holder, fk, mustHex(t, repeat("aa", 32)))
	m.addTx(claim, 100, markerScript("target"), holder)
	// Attacker's later FN02 (height 200), marker-only, no NFT.
	hijack := makeFakeRebind(t, "target", attackerPub, ak, mustHex(t, repeat("bb", 32)))
	m.addTx(hijack, 200, markerScript("target"))

	client := NewElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	got, err := reg.ResolveOwner("target.fn")
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}
	if !bytesEqual(got, ownerPub) {
		t.Fatal("metadata-only FN02 hijacked the name: resolution must follow the NFT commitment, not the latest marker tx")
	}
	if bytesEqual(got, attackerPub) {
		t.Fatal("attacker pubkey was returned")
	}
}
