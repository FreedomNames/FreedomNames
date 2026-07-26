// Package registry defines the seam between a name and the owner that controls
// it. Self-certifying names ("label.<pubKeyID>.fn") carry their owner in the
// name itself and never need a registry; bare names ("mysite.fn") do, and get
// one from a blockchain-backed implementation such as internal/bch.
package registry

import (
	"errors"
	"strings"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// ErrRegistryNotFound is returned when a bare name has no controlling owner
// (including when no name registry is configured, so the name is unclaimable).
var ErrRegistryNotFound = errors.New("name not found in registry")

// NameRegistry maps a bare, human-readable name ("mysite.fn") to the public key
// of its controlling owner. This is the registry seam: self-certifying names
// ("label.<pubKeyID>.fn") never need it, but a blockchain registry can provide
// globally-unique bare names by implementing this interface.
//
// The returned pubKey is a marshaled libp2p public key, identical in form to
// record.FNRecord.PubKey, so the caller can derive the DHT key via record.DHTKeyForPubKey and
// resolve the record set exactly as for a self-certifying name.
type NameRegistry interface {
	// ResolveOwner returns the marshaled owner public key for a bare name.
	// It returns ErrRegistryNotFound if the name is unclaimed.
	ResolveOwner(name string) (pubKey []byte, err error)
}

// IsBareName reports whether a name is a bare "<labels>.fn" name with no
// self-certifying pubkey suffix (i.e. it needs the registry to resolve an
// owner). A name is self-certifying iff its second-to-last label actually
// decodes as a base36 sha2-256 multihash (record.IsPubKeyID) — no length heuristics,
// so long human labels can't be mistaken for key ids and vice versa.
func IsBareName(name string) bool {
	if !strings.HasSuffix(record.CanonicalName(name), "."+record.TLD) {
		return false
	}
	_, keyID, err := record.ParseName(name)
	if err != nil {
		return true
	}
	return !record.IsPubKeyID(keyID)
}
