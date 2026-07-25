package main

import (
	"context"
	"net"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/libp2p/go-libp2p/core/crypto"
)

// fakeDHT is an in-memory RecordStore for tests. It stores raw record bytes keyed
// by DHT key, just like the real DHT.
type fakeDHT struct {
	store map[string][]byte
}

func newFakeDHT() *fakeDHT { return &fakeDHT{store: map[string][]byte{}} }

func (f *fakeDHT) PublishRecord(rec *FNRecord) error {
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

func (f *fakeDHT) ResolveRecord(_ context.Context, key string) (*FNRecord, error) {
	v, ok := f.store[key]
	if !ok {
		return nil, net.ErrClosed // any non-nil error signals "not found"
	}
	return UnmarshalFNRecord(v)
}

func mustResolver(t *testing.T) (*Resolver, crypto.PrivKey, string) {
	t.Helper()
	dhtStore := newFakeDHT()
	cache, _ := NewMemoryCache()
	priv := newTestKey(t)

	rec, err := BuildAndSignRecord(priv, "mysite",
		[]RR{{Type: "A", Value: "10.0.0.5", TTL: 300}}, 1)
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

// TestCNAMEAnsweredForAQuery ensures a CNAME record answers A queries (RFC 1034
// 3.6.2) so CNAME-only names remain reachable through normal clients.
func TestCNAMEAnsweredForAQuery(t *testing.T) {
	rr := RR{Type: "CNAME", Value: "example.com", TTL: 300}
	if got := toDNSRR("site.x.fn.", rr, dns.TypeA); got == nil {
		t.Fatal("expected CNAME record to be returned for an A query")
	}
	if got := toDNSRR("site.x.fn.", rr, dns.TypeTXT); got != nil {
		t.Fatal("did not expect CNAME record for a TXT query")
	}
}

// TestMappedIPv4AnsweredOverDNS ensures the IPv4-mapped IPv6 form accepted by
// record validation also yields a DNS A answer.
func TestMappedIPv4AnsweredOverDNS(t *testing.T) {
	rr := RR{Type: "A", Value: "::ffff:192.0.2.1", TTL: 300}
	got := toDNSRR("site.x.fn.", rr, dns.TypeA)
	if got == nil {
		t.Fatal("expected IPv4-mapped A record to be answered")
	}
	a, ok := got.(*dns.A)
	if !ok || a.Addr.String() != "192.0.2.1" {
		t.Fatalf("expected unmapped 192.0.2.1, got %v", got)
	}
}

// TestDNSServerAnswersFN spins up the real DNS server on an ephemeral UDP port
// and queries it, proving the end-to-end .fn resolution path works over the wire.
func TestDNSServerAnswersFN(t *testing.T) {
	resolver, _, name := mustResolver(t)

	// Find a free port for both UDP and TCP.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()

	srv := NewDNSServer(addr, "127.0.0.1:53", resolver, false)
	if err := srv.Start(); err != nil {
		t.Fatalf("start dns server: %v", err)
	}
	defer srv.Shutdown()

	m := dns.NewMsg(name, dns.TypeA)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := dns.Exchange(ctx, m, "udp", addr)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d (%+v)", len(resp.Answer), resp.Answer)
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("expected A record, got %T", resp.Answer[0])
	}
	if a.Addr.String() != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5, got %s", a.Addr.String())
	}
}
