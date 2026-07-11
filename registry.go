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
// self-certifying pubkey suffix (i.e. it needs the registry to resolve an owner).
// A self-certifying name has the shape "label.<pubKeyID>.fn" where <pubKeyID> is
// a base36 hash; a bare name is anything under .fn that ParseName cannot split
// into (label, keyID).
func isBareName(name string) bool {
	trimmed := strings.TrimSuffix(strings.ToLower(name), ".")
	if !strings.HasSuffix(trimmed, "."+tld) {
		return false
	}
	// If it parses as a self-certifying name AND the keyID looks like a hash,
	// it's not bare. We treat everything that is not a valid self-certifying
	// name as bare and defer to the registry.
	_, keyID, err := ParseName(name)
	if err != nil {
		return true
	}
	// A self-certifying keyID is the base36 of a sha2-256 multihash: length ~52.
	// Treat clearly-too-short suffixes as bare labels rather than key IDs.
	return len(keyID) < 40
}
