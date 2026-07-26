// Package testsupport holds test fixtures shared by more than one package's
// tests. Helpers used by a single package stay in that package's _test.go file;
// only genuinely cross-package ones belong here.
//
// It deliberately imports nothing from this module except internal/record. Go
// forbids import cycles in test binaries too, so anything testsupport imports
// can no longer import testsupport from its own tests — which is why
// internal/record and internal/content keep their own local equivalents.
//
// Named testsupport rather than testutil: "util" says nothing about what is
// inside.
package testsupport

import (
	"context"
	cryptorand "crypto/rand"
	"math/rand"
	"net"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// NewTestKey returns a fresh Ed25519 key for signing test records.
func NewTestKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return priv
}

// TestBytes returns n deterministic pseudo-random bytes.
func TestBytes(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(42)).Read(data)
	return data
}

// FakeDHT is an in-memory resolver.RecordStore for tests. It stores raw record
// bytes keyed by DHT key, just like the real DHT. It satisfies
// resolver.RecordStore structurally, so this package need not import resolver.
type FakeDHT struct {
	store map[string][]byte
}

// NewFakeDHT returns an empty in-memory record store.
func NewFakeDHT() *FakeDHT { return &FakeDHT{store: map[string][]byte{}} }

// PublishRecord marshals and stores a signed record under its DHT key.
func (f *FakeDHT) PublishRecord(rec *record.FNRecord) error {
	key, err := rec.DHTKey()
	if err != nil {
		return err
	}
	value, err := rec.Marshal()
	if err != nil {
		return err
	}
	f.store[key] = value
	return nil
}

// ResolveRecord returns the record stored under key, or an error if absent.
func (f *FakeDHT) ResolveRecord(_ context.Context, key string) (*record.FNRecord, error) {
	v, ok := f.store[key]
	if !ok {
		return nil, net.ErrClosed // any non-nil error signals "not found"
	}
	return record.UnmarshalFNRecord(v)
}
