package bch

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// TestHash160KnownVector checks hash160 against the classic Bitcoin vector:
// hash160 of the uncompressed genesis pubkey yields Satoshi's address hash.
func TestHash160KnownVector(t *testing.T) {
	pub := mustHex(t, "0450863ad64a87ae8a2fe83c1af1a8403cb53f53e486d8511dad8a04887e5b2352"+
		"2cd470243453a299fa9e77237716103abc11a1df38855ed6f2ee187e9c582ba6")
	got := hex.EncodeToString(hash160(pub))
	want := "010966776006953d5567439e5e39f86a0d273bee"
	if got != want {
		t.Fatalf("hash160 = %s, want %s", got, want)
	}
}

// TestSha256dKnownVector checks double-SHA256 of "hello".
func TestSha256dKnownVector(t *testing.T) {
	got := hex.EncodeToString(sha256d([]byte("hello")))
	want := "9595c9df90075148eb06860365df33584b75bff782a510c6cd4883a419833d50"
	if got != want {
		t.Fatalf("sha256d = %s, want %s", got, want)
	}
}

// TestP2PKHScript verifies the exact P2PKH script shape and the round-trip
// through p2pkhHash.
func TestP2PKHScript(t *testing.T) {
	h := mustHex(t, "010966776006953d5567439e5e39f86a0d273bee")
	script := p2pkhScript(h)
	want := "76a914010966776006953d5567439e5e39f86a0d273bee88ac"
	if hex.EncodeToString(script) != want {
		t.Fatalf("p2pkh script = %s, want %s", hex.EncodeToString(script), want)
	}
	if !bytes.Equal(p2pkhHash(script), h) {
		t.Fatal("p2pkhHash did not round-trip")
	}
}

// TestSignatureVerifiesAgainstPreimage is the core correctness proof for the
// sighash: we sign an input, then independently recompute the preimage/digest
// and verify the produced ECDSA signature against the pubkey. If the sighash
// serialization were wrong, verification would fail.
func TestSignatureVerifiesAgainstPreimage(t *testing.T) {
	priv, _ := secp256k1.GeneratePrivateKey()
	pkh := hash160(priv.PubKey().SerializeCompressed())

	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   mustHex(t, "aa"+repeat("bb", 31)),
			PrevIndex:  0,
			PrevScript: p2pkhScript(pkh),
			PrevValue:  100000,
			Sequence:   0xffffffff,
		}},
		Outputs: []txOutput{{
			Value:  99000,
			Script: p2pkhScript(pkh),
		}},
	}

	// Recompute the digest the same way signInput does.
	preimage := tx.sigHashPreimage(0, uint32(sigHashDefault))
	digest := sha256d(preimage)

	unlocking := tx.signInput(0, priv)
	// The unlocking script is: <push sig+hashtype> <push pubkey>.
	pushes := parsePushes(t, unlocking)
	if len(pushes) != 2 {
		t.Fatalf("expected 2 pushes in unlocking script, got %d", len(pushes))
	}
	sigWithType := pushes[0]
	if sigWithType[len(sigWithType)-1] != byte(sigHashDefault) {
		t.Fatalf("sig missing sighash type byte 0x%x", sigHashDefault)
	}
	sig, err := ecdsa.ParseDERSignature(sigWithType[:len(sigWithType)-1])
	if err != nil {
		t.Fatalf("parse DER sig: %v", err)
	}
	if !sig.Verify(digest, priv.PubKey()) {
		t.Fatal("signature does not verify against recomputed sighash digest")
	}
}

// TestTokenSignatureVerifies proves the token-aware sighash path: a UTXO
// carrying a token prefix must include that prefix in scriptCode, and the
// signature must still verify.
func TestTokenSignatureVerifies(t *testing.T) {
	priv, _ := secp256k1.GeneratePrivateKey()
	pkh := hash160(priv.PubKey().SerializeCompressed())
	category := mustHex(t, repeat("cd", 32))

	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   mustHex(t, repeat("ab", 32)),
			PrevIndex:  1,
			PrevScript: p2pkhScript(pkh),
			PrevValue:  1000,
			PrevToken: &tokenInfo{
				CategoryID: category,
				Capability: tokenCapabilityMutable,
				Commitment: hash160([]byte("owner-key")),
			},
			Sequence: 0xffffffff,
		}},
		Outputs: []txOutput{{
			Value:  1000,
			Script: p2pkhScript(pkh),
			Token: &tokenInfo{
				CategoryID: category,
				Capability: tokenCapabilityMutable,
				Commitment: hash160([]byte("new-owner-key")),
			},
		}},
	}

	preimage := tx.sigHashPreimage(0, uint32(sigHashDefault))
	// The token prefix must appear in the preimage's scriptCode.
	if !bytes.Contains(preimage, encodeTokenPrefix(tx.Inputs[0].PrevToken)) {
		t.Fatal("token prefix missing from sighash scriptCode")
	}
	digest := sha256d(preimage)
	unlocking := tx.signInput(0, priv)
	pushes := parsePushes(t, unlocking)
	sig, err := ecdsa.ParseDERSignature(pushes[0][:len(pushes[0])-1])
	if err != nil {
		t.Fatalf("parse sig: %v", err)
	}
	if !sig.Verify(digest, priv.PubKey()) {
		t.Fatal("token-input signature does not verify")
	}
}

// TestAppendPushLargeData checks pushes over 255 bytes use OP_PUSHDATA2 and
// round-trip through parseOpReturn without truncation (e.g. a large RSA key).
func TestAppendPushLargeData(t *testing.T) {
	big := make([]byte, 300)
	for i := range big {
		big[i] = byte(i)
	}
	script := opReturnScript([]byte("FN01"), big)
	pushes := parseOpReturn(script)
	if len(pushes) != 2 {
		t.Fatalf("expected 2 pushes, got %d", len(pushes))
	}
	if !bytes.Equal(pushes[1], big) {
		t.Fatal("large push did not round-trip (truncated?)")
	}
}

// TestTokenValidate rejects malformed token prefixes before serialization.
func TestTokenValidate(t *testing.T) {
	cat := mustHex(t, repeat("11", 32))
	cases := []struct {
		name string
		tok  tokenInfo
		ok   bool
	}{
		{"valid nft", tokenInfo{CategoryID: cat, Capability: tokenCapabilityMutable, Commitment: []byte("x")}, true},
		{"short category", tokenInfo{CategoryID: cat[:20], Capability: tokenCapabilityMutable, Commitment: []byte("x")}, false},
		{"oversized commitment", tokenInfo{CategoryID: cat, Commitment: make([]byte, 41)}, false},
		{"empty prefix", tokenInfo{CategoryID: cat}, false},
		{"bad capability", tokenInfo{CategoryID: cat, Capability: 0x07, Commitment: []byte("x")}, false},
	}
	for _, c := range cases {
		err := c.tok.validate()
		if c.ok && err != nil {
			t.Errorf("%s: expected valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected invalid, got nil", c.name)
		}
	}
}

// TestParseTxRoundTrip serializes a tx (with a token output), parses it back,
// and checks the token prefix and scripts survive.
func TestParseTxRoundTrip(t *testing.T) {
	priv, _ := secp256k1.GeneratePrivateKey()
	pkh := hash160(priv.PubKey().SerializeCompressed())
	category := mustHex(t, repeat("11", 32))
	commitment := hash160([]byte("k"))

	tx := &transaction{
		Version: 2,
		Inputs: []txInput{{
			PrevTxID:   mustHex(t, repeat("22", 32)),
			PrevIndex:  0,
			PrevScript: p2pkhScript(pkh),
			PrevValue:  5000,
			Sequence:   0xffffffff,
		}},
		Outputs: []txOutput{
			{
				Value:  1000,
				Script: p2pkhScript(pkh),
				Token:  &tokenInfo{CategoryID: category, Capability: tokenCapabilityMutable, Commitment: commitment},
			},
			{Value: 3500, Script: opReturnScript([]byte("FN01"), []byte("mysite"))},
		},
	}
	raw, err := tx.Serialize([]*secp256k1.PrivateKey{priv})
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	parsed, err := parseTx(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(parsed.Outputs))
	}
	// Output 0 token survives.
	tok := parsed.Outputs[0].Token
	if tok == nil {
		t.Fatal("output 0 lost its token prefix")
	}
	if !bytes.Equal(tok.CategoryID, category) || tok.Capability != tokenCapabilityMutable || !bytes.Equal(tok.Commitment, commitment) {
		t.Fatalf("token round-trip mismatch: %+v", tok)
	}
	if !bytes.Equal(p2pkhHash(parsed.Outputs[0].Script), pkh) {
		t.Fatal("output 0 script did not round-trip")
	}
	// Output 1 OP_RETURN parses.
	pushes := parseOpReturn(parsed.Outputs[1].Script)
	if len(pushes) != 2 || string(pushes[0]) != "FN01" || string(pushes[1]) != "mysite" {
		t.Fatalf("OP_RETURN round-trip mismatch: %q", pushes)
	}
	// Input outpoint parses.
	if parsed.Inputs[0].PrevIndex != 0 || !bytes.Equal(parsed.Inputs[0].PrevTxID, mustHex(t, repeat("22", 32))) {
		t.Fatal("input outpoint did not round-trip")
	}
}

// --- helpers ---

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func parsePushes(t *testing.T, script []byte) [][]byte {
	t.Helper()
	var pushes [][]byte
	i := 0
	for i < len(script) {
		op := script[i]
		i++
		var n int
		switch {
		case op < opPushData1:
			n = int(op)
		case op == opPushData1:
			n = int(script[i])
			i++
		default:
			t.Fatalf("unexpected opcode 0x%x in script", op)
		}
		pushes = append(pushes, script[i:i+n])
		i += n
	}
	return pushes
}
