package resolver

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// mustResolver builds a Resolver backed by an in-memory store holding one
// signed record for "mysite", and returns it with the owner key and full name.
func mustResolver(t *testing.T) (*Resolver, crypto.PrivKey, string) {
	t.Helper()
	dhtStore := testsupport.NewFakeDHT()
	cache, _ := NewMemoryCache()
	priv := testsupport.NewTestKey(t)

	rec, err := record.BuildAndSignRecord(priv, "mysite",
		[]record.RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
	if err != nil {
		t.Fatalf("build record: %v", err)
	}
	if err := dhtStore.PublishRecord(rec); err != nil {
		t.Fatalf("publish: %v", err)
	}
	name, _ := rec.FullName()
	return NewResolver(dhtStore, cache), priv, name
}

func TestResolverResolvesFNName(t *testing.T) {
	resolver, _, name := mustResolver(t)

	records, err := resolver.Resolve(context.Background(), name)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if len(records) != 1 || records[0].Value != "10.0.0.5" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

// TestResolverNormalizesCacheKey ensures the DNS FQDN spelling (trailing dot,
// mixed case) and the plain spelling share one cache entry and both resolve.
func TestResolverNormalizesCacheKey(t *testing.T) {
	resolver, _, name := mustResolver(t)

	// Prime the cache using the FQDN mixed-case spelling.
	fqdn := "MySite." + name[len("mysite."):] + "." // e.g. MySite.<id>.fn.
	if _, err := resolver.Resolve(context.Background(), fqdn); err != nil {
		t.Fatalf("resolve fqdn spelling: %v", err)
	}
	// The canonical spelling must hit the same (single) cache entry.
	if _, err := resolver.Resolve(context.Background(), name); err != nil {
		t.Fatalf("resolve canonical spelling: %v", err)
	}
	if got := resolver.cache.Length(); got != 1 {
		t.Fatalf("expected 1 shared cache entry across spellings, got %d", got)
	}
}
