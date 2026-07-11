package main

import "testing"

// TestCashAddrKnownVectors checks encoding against canonical spec vectors from
// the cashaddr specification (mainnet P2PKH) and the testnet prefix.
func TestCashAddrKnownVectors(t *testing.T) {
	// From the cashaddr spec test vectors: hash160 -> mainnet P2PKH address.
	cases := []struct {
		network string
		hexHash string
		want    string
	}{
		{
			// The canonical cashaddr spec example.
			network: "mainnet",
			hexHash: "76A04053BDA0A88BDA5177B86A15C3B29F559873",
			want:    "bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a",
		},
		{
			network: "mainnet",
			hexHash: "011F28E473C95F4013D7D53EC5FBC3B42DF8ED10",
			want:    "bitcoincash:qqq3728yw0y47sqn6l2na30mcw6zm78dzqre909m2r",
		},
	}
	for _, c := range cases {
		got := encodeCashAddr(c.network, cashAddrP2PKH, mustHex(t, c.hexHash))
		if got != c.want {
			t.Errorf("encodeCashAddr(%s) = %s, want %s", c.hexHash, got, c.want)
		}
	}

	// Testnet/chipnet uses the bchtest prefix.
	got := encodeCashAddr("chipnet", cashAddrP2PKH, mustHex(t, "76A04053BDA0A88BDA5177B86A15C3B29F559873"))
	if got[:7] != "bchtest" {
		t.Errorf("chipnet address should start with bchtest, got %s", got)
	}
}
