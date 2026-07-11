package main

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// mockRegistry maps bare names to owner pubkeys for testing the Layer 2 seam.
type mockRegistry struct {
	owners map[string][]byte
}

func (m *mockRegistry) ResolveOwner(name string) ([]byte, error) {
	if pub, ok := m.owners[name]; ok {
		return pub, nil
	}
	return nil, ErrRegistryNotFound
}

func TestIsBareName(t *testing.T) {
	cases := map[string]bool{
		"mysite.fn": true, // bare
		"mysite.mugh925ipvygve5a4p0p8ai5vp4o2dofmeeok84hamb238j2r9o3.fn": false, // self-certifying
		"example.com": false, // not .fn
	}
	for name, want := range cases {
		if got := isBareName(name); got != want {
			t.Errorf("isBareName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestBareNameResolvesViaRegistry(t *testing.T) {
	dhtStore := newFakeDHT()
	cache, _ := NewMemoryCache()
	priv := newTestKey(t)

	// Publish the owner's record set (as Layer 1 would).
	rec, err := BuildAndSignRecord(priv, "mysite",
		[]RR{{Type: "A", Value: "10.0.0.9", TTL: 300}}, 1)
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
	records, err := resolver.Resolve("mysite.fn")
	if err != nil {
		t.Fatalf("resolve bare name: %v", err)
	}
	if len(records) != 1 || records[0].Value != "10.0.0.9" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestBareNameStubFallsBack(t *testing.T) {
	dhtStore := newFakeDHT()
	cache, _ := NewMemoryCache()

	// The BCH stub is not implemented: bare names must error, not panic.
	resolver := NewResolver(dhtStore, cache).WithRegistry(NewBCHRegistry())
	if _, err := resolver.Resolve("mysite.fn"); err == nil {
		t.Fatal("expected bare-name resolution to fail against the stub")
	}
}
