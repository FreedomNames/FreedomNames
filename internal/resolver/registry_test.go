package resolver

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/registry"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// mockRegistry maps bare names to owner pubkeys for testing the registry seam.
type mockRegistry struct {
	owners map[string][]byte
}

func (m *mockRegistry) ResolveOwner(name string) ([]byte, error) {
	if pub, ok := m.owners[name]; ok {
		return pub, nil
	}
	return nil, registry.ErrRegistryNotFound
}

func TestBareNameResolvesViaRegistry(t *testing.T) {
	dhtStore := testsupport.NewFakeDHT()
	cache, _ := NewMemoryCache()
	priv := testsupport.NewTestKey(t)

	// Publish the owner's record set (as the key layer would).
	rec, err := record.BuildAndSignRecord(priv, "mysite",
		[]record.RR{{Type: "A", Value: "10.0.0.9", TTL: 300}}, 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := dhtStore.PublishRecord(rec); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The registry knows the bare name "mysite.fn" maps to this owner.
	pub, _ := crypto.MarshalPublicKey(priv.GetPublic())
	reg := &mockRegistry{owners: map[string][]byte{"mysite.fn": pub}}

	resolver := NewResolver(dhtStore, cache).WithRegistry(reg)
	records, err := resolver.Resolve(context.Background(), "mysite.fn")
	if err != nil {
		t.Fatalf("resolve bare name: %v", err)
	}
	if len(records) != 1 || records[0].Value != "10.0.0.9" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestBareNameUnknownIsNotFound(t *testing.T) {
	dhtStore := testsupport.NewFakeDHT()
	cache, _ := NewMemoryCache()

	// A registry with no claims returns not-found for a bare name.
	reg := &mockRegistry{owners: map[string][]byte{}}
	resolver := NewResolver(dhtStore, cache).WithRegistry(reg)
	if _, err := resolver.Resolve(context.Background(), "mysite.fn"); err == nil {
		t.Fatal("expected bare-name resolution to fail when unclaimed")
	}
}
