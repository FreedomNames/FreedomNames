package main

import (
	"errors"
	"strings"
)

// ErrRegistryNotFound is returned when a bare name has no controlling owner.
var ErrRegistryNotFound = errors.New("name not found in registry")

// ErrRegistryNotImplemented is returned by resolvers that are not yet built.
var ErrRegistryNotImplemented = errors.New("registry not implemented")

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
