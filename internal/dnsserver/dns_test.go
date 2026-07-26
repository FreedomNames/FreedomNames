package dnsserver

import (
	"context"
	"net"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/libp2p/go-libp2p/core/crypto"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/resolver"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

func mustResolver(t *testing.T) (*resolver.Resolver, crypto.PrivKey, string) {
	t.Helper()
	dhtStore := testsupport.NewFakeDHT()
	cache, _ := resolver.NewMemoryCache()
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
	return resolver.NewResolver(dhtStore, cache), priv, name
}

// TestCNAMEAnsweredForAQuery ensures a CNAME record answers A queries (RFC 1034
// 3.6.2) so CNAME-only names remain reachable through normal clients.
func TestCNAMEAnsweredForAQuery(t *testing.T) {
	rr := record.RR{Type: "CNAME", Value: "example.com", TTL: 300}
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
	rr := record.RR{Type: "A", Value: "::ffff:192.0.2.1", TTL: 300}
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
