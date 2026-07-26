package bch

import "encoding/hex"

// CashAddr encoding (display only — all chain queries use script hashes). See
// https://github.com/bitcoincashorg/bitcoincash.org/blob/master/spec/cashaddr.md

const cashAddrCharset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// Address type nibble (upper bits of the version byte).
const (
	cashAddrP2PKH = 0
	cashAddrP2SH  = 1
)

// cashAddrPrefix returns the human-readable prefix for a network. Chipnet uses
// the testnet prefix "bchtest".
func cashAddrPrefix(network string) string {
	switch network {
	case "mainnet":
		return "bitcoincash"
	default: // chipnet / testnet4 / testnet
		return "bchtest"
	}
}

// encodeCashAddr builds a cashaddr string for a 20-byte hash160.
func encodeCashAddr(network string, addrType byte, hash160 []byte) string {
	prefix := cashAddrPrefix(network)

	// Version byte: addrType in bits 6..3, size code in bits 2..0.
	// Size code 0 = 160 bits (20 bytes), which is all we emit.
	versionByte := addrType << 3
	payload := append([]byte{versionByte}, hash160...)

	// Convert the 8-bit payload to 5-bit groups.
	data := convertBits(payload, 8, 5, true)

	// Checksum is computed over: prefix (low 5 bits of each char), a 0
	// separator, the data, and 8 zero template groups.
	var checksumInput []byte
	for i := 0; i < len(prefix); i++ {
		checksumInput = append(checksumInput, prefix[i]&0x1f)
	}
	checksumInput = append(checksumInput, 0) // separator
	checksumInput = append(checksumInput, data...)
	checksumInput = append(checksumInput, make([]byte, 8)...) // template

	mod := polymod(checksumInput)
	checksum := make([]byte, 8)
	for i := 0; i < 8; i++ {
		checksum[i] = byte((mod >> uint(5*(7-i))) & 0x1f)
	}

	var out []byte
	out = append(out, prefix...)
	out = append(out, ':')
	for _, d := range data {
		out = append(out, cashAddrCharset[d])
	}
	for _, c := range checksum {
		out = append(out, cashAddrCharset[c])
	}
	return string(out)
}

// polymod is the CashAddr BCH checksum over 5-bit groups.
func polymod(data []byte) uint64 {
	var c uint64 = 1
	for _, d := range data {
		c0 := byte(c >> 35)
		c = ((c & 0x07ffffffff) << 5) ^ uint64(d)
		if c0&0x01 != 0 {
			c ^= 0x98f2bc8e61
		}
		if c0&0x02 != 0 {
			c ^= 0x79b76d99e2
		}
		if c0&0x04 != 0 {
			c ^= 0xf33e5fb3c4
		}
		if c0&0x08 != 0 {
			c ^= 0xae2eabe2a8
		}
		if c0&0x10 != 0 {
			c ^= 0x1e4f43e470
		}
	}
	return c ^ 1
}

// convertBits regroups a byte slice from `from`-bit groups to `to`-bit groups.
func convertBits(data []byte, from, to uint, pad bool) []byte {
	var acc uint32
	var bits uint
	var out []byte
	maxv := uint32((1 << to) - 1)
	for _, b := range data {
		acc = (acc << from) | uint32(b)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte((acc>>bits)&maxv))
		}
	}
	if pad && bits > 0 {
		out = append(out, byte((acc<<(to-bits))&maxv))
	}
	return out
}

// decodeHexLenient decodes hex, tolerating an odd 0x prefix.
func decodeHexLenient(s string) ([]byte, error) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	return hex.DecodeString(s)
}
