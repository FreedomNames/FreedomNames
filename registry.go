package main

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRegistryNotFound is returned when a bare name has no controlling owner
// (including when no Layer 2 registry is configured, so the name is unclaimable).
var ErrRegistryNotFound = errors.New("name not found in registry")

// FN protocol tags carried in the OP_RETURN of registry transactions.
const (
	fnClaimTag  = "FN01" // mints the name NFT + reveals owner pubkey
	fnRebindTag = "FN02" // rebinds an existing name NFT to a new owner pubkey
)

// normalizeRegistryName normalizes a bare label for on-chain registration:
// lowercase, charset [a-z0-9-], no leading/trailing '-', length 1..63. It
// accepts either a bare label ("mysite") or a full "mysite.fn" name.
func normalizeRegistryName(name string) (string, error) {
	label := strings.TrimSuffix(CanonicalName(name), "."+tld)
	if label == "" {
		return "", fmt.Errorf("%w: empty name", ErrNotFNName)
	}
	if len(label) > 63 {
		return "", fmt.Errorf("%w: name too long (max 63)", ErrNotFNName)
	}
	if strings.Contains(label, ".") {
		return "", fmt.Errorf("%w: bare names cannot contain dots", ErrNotFNName)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return "", fmt.Errorf("%w: name cannot start or end with '-'", ErrNotFNName)
	}
	for _, c := range label {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "", fmt.Errorf("%w: illegal character %q (use a-z 0-9 -)", ErrNotFNName, c)
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

// NameRegistry maps a bare, human-readable name ("mysite.fn") to the public key
// of its controlling owner. This is the Layer 2 seam: Layer 1 (self-certifying
// "label.<pubKeyID>.fn" names) never needs it, but a blockchain registry can
// provide globally-unique bare names by implementing this interface.
//
// The returned pubKey is a marshaled libp2p public key, identical in form to
// FNRecord.PubKey, so the caller can derive the DHT key via DHTKeyForPubKey and
// resolve the record set exactly as for a self-certifying name.
type NameRegistry interface {
	// ResolveOwner returns the marshaled owner public key for a bare name.
	// It returns ErrRegistryNotFound if the name is unclaimed.
	ResolveOwner(name string) (pubKey []byte, err error)
}

// isBareName reports whether a name is a bare "<labels>.fn" name with no
// self-certifying pubkey suffix (i.e. it needs the registry to resolve an
// owner). A name is self-certifying iff its second-to-last label actually
// decodes as a base36 sha2-256 multihash (IsPubKeyID) — no length heuristics,
// so long human labels can't be mistaken for key ids and vice versa.
func isBareName(name string) bool {
	if !strings.HasSuffix(CanonicalName(name), "."+tld) {
		return false
	}
	_, keyID, err := ParseName(name)
	if err != nil {
		return true
	}
	return !IsPubKeyID(keyID)
}
