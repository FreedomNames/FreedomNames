package bch

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // BCH hash160 requires RIPEMD-160
)

// This file implements just enough Bitcoin Cash transaction machinery, in pure
// Go, to build/sign/serialize the transactions the Freedom Names registry
// needs — including CashTokens (token prefix) outputs and the token-aware
// BIP143 + SIGHASH_FORKID signing serialization. It intentionally avoids a
// heavy chain dependency. The serialization and sighash are validated against
// public test vectors in bchtx_test.go before any of this is broadcast.

// Bitcoin Cash sighash flags (BIP143-style, with the mandatory FORKID bit).
const (
	sigHashAll     = 0x01
	sigHashForkID  = 0x40
	sigHashDefault = sigHashAll | sigHashForkID
)

// dustLimit is the minimum satoshi value for a standard P2PKH output.
const dustLimit = 546

// tokenDustLimit is a safe minimum for an output carrying a CashTokens NFT
// prefix. The BCH dust threshold scales with the serialized output size
// (~3*(size+148)); a P2PKH output with a token prefix + 20-byte commitment is
// ~89 bytes, giving ~711 sat. 1000 clears it with margin.
const tokenDustLimit = 1000

// Script opcodes we emit.
const (
	opDup         = 0x76
	opHash160     = 0xa9
	opEqualVerify = 0x88
	opCheckSig    = 0xac
	opEqual       = 0x87
	opReturn      = 0x6a
	opPushData1   = 0x4c
	opPushData2   = 0x4d
)

// CashTokens token-prefix constants (per the CashTokens CHIP).
const (
	tokenPrefixByte       = 0xef
	tokenHasCommitmentLen = 0x40
	tokenHasNFT           = 0x20
	tokenHasAmount        = 0x10

	tokenCapabilityNone    = 0x00
	tokenCapabilityMutable = 0x01
	tokenCapabilityMinting = 0x02
)

// hash160 = RIPEMD160(SHA256(b)), the standard Bitcoin address/commitment hash.
func hash160(b []byte) []byte {
	sha := sha256.Sum256(b)
	r := ripemd160.New()
	r.Write(sha[:])
	return r.Sum(nil)
}

// sha256d = double SHA-256, used for txids and sighashes.
func sha256d(b []byte) []byte {
	first := sha256.Sum256(b)
	second := sha256.Sum256(first[:])
	return second[:]
}

// tokenInfo describes the CashTokens data attached to an output (or UTXO).
type tokenInfo struct {
	CategoryID []byte // 32 bytes, internal (little-endian / reversed-display) order
	Capability byte   // tokenCapabilityNone|Mutable|Minting
	Commitment []byte // NFT commitment (0..40 bytes)
	Amount     uint64 // fungible amount (0 = none)
}

// txInput is a transaction input to be signed.
type txInput struct {
	PrevTxID   []byte     // 32 bytes, internal order
	PrevIndex  uint32     // output index in the previous tx
	PrevScript []byte     // locking script of the UTXO being spent (P2PKH)
	PrevValue  int64      // satoshi value of the UTXO
	PrevToken  *tokenInfo // token prefix of the UTXO, if it carries tokens
	Sequence   uint32
}

// txOutput is a transaction output.
type txOutput struct {
	Value  int64      // satoshis
	Script []byte     // locking script
	Token  *tokenInfo // optional CashTokens prefix
}

// transaction is a minimal BCH transaction.
type transaction struct {
	Version  int32
	Inputs   []txInput
	Outputs  []txOutput
	LockTime uint32
}

// --- low-level encoders ---

// putVarInt appends a Bitcoin CompactSize integer.
func putVarInt(buf *bytes.Buffer, n uint64) {
	switch {
	case n < 0xfd:
		buf.WriteByte(byte(n))
	case n <= 0xffff:
		buf.WriteByte(0xfd)
		binary.Write(buf, binary.LittleEndian, uint16(n))
	case n <= 0xffffffff:
		buf.WriteByte(0xfe)
		binary.Write(buf, binary.LittleEndian, uint32(n))
	default:
		buf.WriteByte(0xff)
		binary.Write(buf, binary.LittleEndian, n)
	}
}

// putVarBytes appends a length-prefixed byte string.
func putVarBytes(buf *bytes.Buffer, b []byte) {
	putVarInt(buf, uint64(len(b)))
	buf.Write(b)
}

// p2pkhScript builds a standard pay-to-pubkey-hash locking script for a
// 20-byte hash160.
func p2pkhScript(pubKeyHash []byte) []byte {
	s := make([]byte, 0, 25)
	s = append(s, opDup, opHash160, byte(len(pubKeyHash)))
	s = append(s, pubKeyHash...)
	s = append(s, opEqualVerify, opCheckSig)
	return s
}

// opReturnScript builds an OP_RETURN script carrying the given data pushes.
func opReturnScript(pushes ...[]byte) []byte {
	s := []byte{opReturn}
	for _, p := range pushes {
		s = appendPush(s, p)
	}
	return s
}

// appendPush appends a minimal data push for p, supporting the full standard
// push range (0..520 bytes) via direct pushes, OP_PUSHDATA1 and OP_PUSHDATA2,
// so a larger (e.g. RSA) key can never be silently truncated.
func appendPush(s, p []byte) []byte {
	switch {
	case len(p) < int(opPushData1):
		s = append(s, byte(len(p)))
	case len(p) <= 0xff:
		s = append(s, opPushData1, byte(len(p)))
	default:
		s = append(s, opPushData2, byte(len(p)), byte(len(p)>>8))
	}
	return append(s, p...)
}

// maxCommitmentLen is the CashTokens consensus limit on NFT commitment length.
const maxCommitmentLen = 40

// validate checks a token prefix against the CashTokens consensus rules so we
// never serialize (and broadcast) a token that a node would reject.
func (t *tokenInfo) validate() error {
	if len(t.CategoryID) != 32 {
		return fmt.Errorf("token category must be 32 bytes, got %d", len(t.CategoryID))
	}
	if len(t.Commitment) > maxCommitmentLen {
		return fmt.Errorf("token commitment %d bytes exceeds max %d", len(t.Commitment), maxCommitmentLen)
	}
	hasNFT := t.Capability != tokenCapabilityNone || len(t.Commitment) > 0
	if !hasNFT && t.Amount == 0 {
		return errors.New("token prefix has neither an NFT nor a fungible amount")
	}
	switch t.Capability {
	case tokenCapabilityNone, tokenCapabilityMutable, tokenCapabilityMinting:
	default:
		return fmt.Errorf("invalid token capability 0x%x", t.Capability)
	}
	return nil
}

// encodeTokenPrefix serializes a CashTokens token prefix (without the leading
// output value / script length — those are handled by the output encoder).
func encodeTokenPrefix(t *tokenInfo) []byte {
	var buf bytes.Buffer
	buf.WriteByte(tokenPrefixByte)
	buf.Write(t.CategoryID) // 32 bytes, internal order

	bitfield := byte(0)
	if t.Capability != tokenCapabilityNone || len(t.Commitment) > 0 {
		bitfield |= tokenHasNFT
	}
	if len(t.Commitment) > 0 {
		bitfield |= tokenHasCommitmentLen
	}
	if t.Amount > 0 {
		bitfield |= tokenHasAmount
	}
	bitfield |= t.Capability & 0x0f
	buf.WriteByte(bitfield)

	if len(t.Commitment) > 0 {
		putVarInt(&buf, uint64(len(t.Commitment)))
		buf.Write(t.Commitment)
	}
	if t.Amount > 0 {
		putVarInt(&buf, t.Amount)
	}
	return buf.Bytes()
}

// encodeOutput serializes one output: value, then a CompactSize covering the
// token prefix (if any) plus the locking bytecode, then those bytes.
func encodeOutput(buf *bytes.Buffer, o txOutput) {
	binary.Write(buf, binary.LittleEndian, o.Value)
	var wrapped []byte
	if o.Token != nil {
		wrapped = append(wrapped, encodeTokenPrefix(o.Token)...)
	}
	wrapped = append(wrapped, o.Script...)
	putVarBytes(buf, wrapped)
}

// --- BIP143 + FORKID sighash (token-aware) ---
//
// Preimage layout (BCH replay-protected sighash):
//   version | hashPrevouts | hashSequence | outpoint |
//   scriptCode | value | sequence | hashOutputs | locktime | sighashType
// For a token UTXO, the full encoded token prefix is prepended to the covered
// bytecode inside scriptCode (per the CashTokens CHIP).

func (tx *transaction) hashPrevouts() []byte {
	var buf bytes.Buffer
	for _, in := range tx.Inputs {
		buf.Write(in.PrevTxID)
		binary.Write(&buf, binary.LittleEndian, in.PrevIndex)
	}
	return sha256d(buf.Bytes())
}

func (tx *transaction) hashSequence() []byte {
	var buf bytes.Buffer
	for _, in := range tx.Inputs {
		binary.Write(&buf, binary.LittleEndian, in.Sequence)
	}
	return sha256d(buf.Bytes())
}

func (tx *transaction) hashOutputs() []byte {
	var buf bytes.Buffer
	for _, o := range tx.Outputs {
		encodeOutput(&buf, o)
	}
	return sha256d(buf.Bytes())
}

// sigHashPreimage builds the preimage for input i.
func (tx *transaction) sigHashPreimage(i int, sigHashType uint32) []byte {
	in := tx.Inputs[i]

	// scriptCode: for token UTXOs, prepend the encoded token prefix.
	var scriptCode []byte
	if in.PrevToken != nil {
		scriptCode = append(scriptCode, encodeTokenPrefix(in.PrevToken)...)
	}
	scriptCode = append(scriptCode, in.PrevScript...)

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, tx.Version)
	buf.Write(tx.hashPrevouts())
	buf.Write(tx.hashSequence())
	buf.Write(in.PrevTxID)
	binary.Write(&buf, binary.LittleEndian, in.PrevIndex)
	putVarBytes(&buf, scriptCode)
	binary.Write(&buf, binary.LittleEndian, in.PrevValue)
	binary.Write(&buf, binary.LittleEndian, in.Sequence)
	buf.Write(tx.hashOutputs())
	binary.Write(&buf, binary.LittleEndian, tx.LockTime)
	binary.Write(&buf, binary.LittleEndian, sigHashType)
	return buf.Bytes()
}

// signInput produces the P2PKH unlocking script (sig + pubkey) for input i.
func (tx *transaction) signInput(i int, priv *secp256k1.PrivateKey) []byte {
	sigHashType := uint32(sigHashDefault)
	preimage := tx.sigHashPreimage(i, sigHashType)
	digest := sha256d(preimage)

	sig := ecdsa.Sign(priv, digest)
	sigBytes := append(sig.Serialize(), byte(sigHashType))

	pubKey := priv.PubKey().SerializeCompressed()

	var script []byte
	script = appendPush(script, sigBytes)
	script = appendPush(script, pubKey)
	return script
}

// Serialize returns the fully-signed raw transaction bytes. Each input is
// signed with the matching key from privs (indexed the same as tx.Inputs).
func (tx *transaction) Serialize(privs []*secp256k1.PrivateKey) ([]byte, error) {
	if len(privs) != len(tx.Inputs) {
		return nil, fmt.Errorf("have %d keys for %d inputs", len(privs), len(tx.Inputs))
	}
	// Reject invalid token outputs before signing so we never broadcast a tx a
	// node would reject as a malformed token.
	for i, o := range tx.Outputs {
		if o.Token != nil {
			if err := o.Token.validate(); err != nil {
				return nil, fmt.Errorf("output %d token: %w", i, err)
			}
		}
	}
	unlocking := make([][]byte, len(tx.Inputs))
	for i := range tx.Inputs {
		unlocking[i] = tx.signInput(i, privs[i])
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, tx.Version)
	putVarInt(&buf, uint64(len(tx.Inputs)))
	for i, in := range tx.Inputs {
		buf.Write(in.PrevTxID)
		binary.Write(&buf, binary.LittleEndian, in.PrevIndex)
		putVarBytes(&buf, unlocking[i])
		binary.Write(&buf, binary.LittleEndian, in.Sequence)
	}
	putVarInt(&buf, uint64(len(tx.Outputs)))
	for _, o := range tx.Outputs {
		encodeOutput(&buf, o)
	}
	binary.Write(&buf, binary.LittleEndian, tx.LockTime)
	return buf.Bytes(), nil
}

// TxID returns the transaction id (double-SHA256 of the serialized tx) in
// internal (little-endian) order. Callers reverse it for display.
func txID(rawTx []byte) []byte {
	return sha256d(rawTx)
}

// reverseBytes returns a reversed copy (internal <-> display order for hashes).
func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

// --- raw transaction parsing (for reading claims off-chain) ---

// parsedTx is the decoded form of a raw transaction we fetched, exposing only
// what the registry needs: outputs (with token prefixes and scripts) and the
// inputs' outpoints (to walk custody chains).
type parsedTx struct {
	Inputs  []parsedInput
	Outputs []parsedOutput
}

type parsedInput struct {
	PrevTxID  []byte // internal order
	PrevIndex uint32
}

type parsedOutput struct {
	Value  int64
	Script []byte
	Token  *tokenInfo // nil if no token prefix
}

// parseTx decodes a raw BCH transaction, including CashTokens output prefixes.
func parseTx(raw []byte) (*parsedTx, error) {
	r := &byteReader{buf: raw}
	tx := &parsedTx{}

	if _, err := r.readUint32(); err != nil { // version
		return nil, err
	}
	nIn, err := r.readVarInt()
	if err != nil {
		return nil, err
	}
	for i := uint64(0); i < nIn; i++ {
		prevID, err := r.readBytes(32)
		if err != nil {
			return nil, err
		}
		idx, err := r.readUint32()
		if err != nil {
			return nil, err
		}
		script, err := r.readVarBytes()
		if err != nil {
			return nil, err
		}
		_ = script
		if _, err := r.readUint32(); err != nil { // sequence
			return nil, err
		}
		tx.Inputs = append(tx.Inputs, parsedInput{PrevTxID: prevID, PrevIndex: idx})
	}

	nOut, err := r.readVarInt()
	if err != nil {
		return nil, err
	}
	for i := uint64(0); i < nOut; i++ {
		value, err := r.readInt64()
		if err != nil {
			return nil, err
		}
		wrapped, err := r.readVarBytes()
		if err != nil {
			return nil, err
		}
		token, script, err := splitTokenPrefix(wrapped)
		if err != nil {
			return nil, err
		}
		tx.Outputs = append(tx.Outputs, parsedOutput{Value: value, Script: script, Token: token})
	}
	return tx, nil
}

// splitTokenPrefix separates an optional CashTokens prefix from the locking
// bytecode that follows it.
func splitTokenPrefix(wrapped []byte) (*tokenInfo, []byte, error) {
	if len(wrapped) == 0 || wrapped[0] != tokenPrefixByte {
		return nil, wrapped, nil
	}
	r := &byteReader{buf: wrapped}
	r.pos = 1 // skip prefix byte
	category, err := r.readBytes(32)
	if err != nil {
		return nil, nil, err
	}
	bitfield, err := r.readByte()
	if err != nil {
		return nil, nil, err
	}
	// The reserved high bit must be unset; the NFT-capability nibble is only
	// meaningful when HAS_NFT is set.
	if bitfield&0x80 != 0 {
		return nil, nil, errors.New("token bitfield reserved bit set")
	}
	t := &tokenInfo{CategoryID: category}
	if bitfield&tokenHasNFT != 0 {
		t.Capability = bitfield & 0x0f
	}
	if bitfield&tokenHasCommitmentLen != 0 {
		n, err := r.readVarInt()
		if err != nil {
			return nil, nil, err
		}
		if n == 0 || n > maxCommitmentLen {
			return nil, nil, fmt.Errorf("invalid token commitment length %d", n)
		}
		commitment, err := r.readBytes(int(n))
		if err != nil {
			return nil, nil, err
		}
		t.Commitment = commitment
	}
	if bitfield&tokenHasAmount != 0 {
		amt, err := r.readVarInt()
		if err != nil {
			return nil, nil, err
		}
		t.Amount = amt
	}
	return t, wrapped[r.pos:], nil
}

// p2pkhHash extracts the 20-byte pubkey hash from a standard P2PKH script,
// or nil if the script is not P2PKH.
func p2pkhHash(script []byte) []byte {
	if len(script) == 25 && script[0] == opDup && script[1] == opHash160 &&
		script[2] == 20 && script[23] == opEqualVerify && script[24] == opCheckSig {
		return script[3:23]
	}
	return nil
}

// parseOpReturn returns the data pushes of an OP_RETURN script, or nil if the
// script is not an OP_RETURN.
func parseOpReturn(script []byte) [][]byte {
	if len(script) == 0 || script[0] != opReturn {
		return nil
	}
	r := &byteReader{buf: script}
	r.pos = 1
	var pushes [][]byte
	for r.pos < len(script) {
		op, err := r.readByte()
		if err != nil {
			return pushes
		}
		var n int
		switch {
		case op < opPushData1:
			n = int(op)
		case op == opPushData1:
			b, err := r.readByte()
			if err != nil {
				return pushes
			}
			n = int(b)
		case op == opPushData2:
			lo, err := r.readByte()
			if err != nil {
				return pushes
			}
			hi, err := r.readByte()
			if err != nil {
				return pushes
			}
			n = int(lo) | int(hi)<<8
		default:
			return pushes // OP_PUSHDATA4 not used by us
		}
		data, err := r.readBytes(n)
		if err != nil {
			return pushes
		}
		pushes = append(pushes, data)
	}
	return pushes
}

// --- tiny byte reader ---

type byteReader struct {
	buf []byte
	pos int
}

var errShortBuffer = errors.New("unexpected end of transaction bytes")

func (r *byteReader) readByte() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, errShortBuffer
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *byteReader) readBytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, errShortBuffer
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *byteReader) readUint32() (uint32, error) {
	b, err := r.readBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *byteReader) readInt64() (int64, error) {
	b, err := r.readBytes(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b)), nil
}

func (r *byteReader) readVarInt() (uint64, error) {
	b, err := r.readByte()
	if err != nil {
		return 0, err
	}
	switch b {
	case 0xfd:
		v, err := r.readBytes(2)
		if err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint16(v)), nil
	case 0xfe:
		v, err := r.readBytes(4)
		if err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint32(v)), nil
	case 0xff:
		v, err := r.readBytes(8)
		if err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint64(v), nil
	default:
		return uint64(b), nil
	}
}

func (r *byteReader) readVarBytes() ([]byte, error) {
	n, err := r.readVarInt()
	if err != nil {
		return nil, err
	}
	return r.readBytes(int(n))
}
