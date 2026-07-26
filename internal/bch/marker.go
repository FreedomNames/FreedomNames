package bch

import (
	"fmt"
	"strings"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// FN protocol tags carried in the OP_RETURN of registry transactions.
const (
	fnClaimTag  = "FN01" // mints the name NFT + reveals owner pubkey
	fnRebindTag = "FN02" // rebinds an existing name NFT to a new owner pubkey
)

// NormalizeName normalizes a bare label for on-chain registration:
// lowercase, charset [a-z0-9-], no leading/trailing '-', length 1..63. It
// accepts either a bare label ("mysite") or a full "mysite.fn" name.
func NormalizeName(name string) (string, error) {
	label := strings.TrimSuffix(record.CanonicalName(name), "."+record.TLD)
	if label == "" {
		return "", fmt.Errorf("%w: empty name", record.ErrNotFNName)
	}
	if len(label) > 63 {
		return "", fmt.Errorf("%w: name too long (max 63)", record.ErrNotFNName)
	}
	if strings.Contains(label, ".") {
		return "", fmt.Errorf("%w: bare names cannot contain dots", record.ErrNotFNName)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return "", fmt.Errorf("%w: name cannot start or end with '-'", record.ErrNotFNName)
	}
	for _, c := range label {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "", fmt.Errorf("%w: illegal character %q (use a-z 0-9 -)", record.ErrNotFNName, c)
		}
	}
	return label, nil
}

// markerPubKeyHash returns the deterministic, no-key P2PKH pubkey-hash used as
// the discovery marker for a normalized name: hash160("FN:" + name). Claim and
// rebind transactions each pay a dust output here so they are all findable via
// a single scripthash history query.
func markerPubKeyHash(normalizedName string) []byte {
	return hash160([]byte("FN:" + normalizedName))
}

// markerScript returns the P2PKH locking script for a name's discovery marker.
func markerScript(normalizedName string) []byte {
	return p2pkhScript(markerPubKeyHash(normalizedName))
}
