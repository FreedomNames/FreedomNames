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

func (f *fakeDHT) ResolveRecord(key string) (*FNRecord, error) {
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

	records, err := resolver.Resolve(name)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if len(records) != 1 || records[0].Value != "10.0.0.5" {
		t.Fatalf("unexpected records: %+v", records)
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

	srv := NewDNSServer(addr, "127.0.0.1:53", resolver)
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
