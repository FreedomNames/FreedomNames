package dnsserver

import (
	"bytes"
	"log"
	"net"
	"os"
	"strings"
	"testing"
)

// --- DNS server: open-resolver gate and log-injection hardening ---

func TestForwardingAllowedOnlyForLocalClients(t *testing.T) {
	cases := []struct {
		addr net.Addr
		want bool
	}{
		{&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("::1"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("192.168.1.20"), Port: 5000}, true},
		{&net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("169.254.1.1"), Port: 5000}, true},
		// The open-resolver case: a stranger on the internet must not be able
		// to bounce arbitrary queries off this node.
		{&net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 5000}, false},
		{&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5000}, false},
		{&net.UDPAddr{IP: net.ParseIP("2606:4700::1111"), Port: 5000}, false},
	}
	for _, c := range cases {
		if got := forwardingAllowed(c.addr); got != c.want {
			t.Errorf("forwardingAllowed(%v) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// --- log integrity ---

// TestDNSLogsEscapeHostileQueryNames covers log forgery from an unauthenticated
// packet. A query name is raw bytes off the wire — the DNS library escapes
// embedded dots and nothing else — and the .fn zone answers whoever asks, by
// design. Logging one with %s would let anyone write whatever they liked into
// the operator's log, including terminal escapes.
func TestDNSLogsEscapeHostileQueryNames(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	hostile := "evil\n2026/01/01 00:00:00 DNS server listening on 0.0.0.0:53\x1b[2Kgotcha.fn."
	s := &DNSServer{inflight: make(chan struct{}, 1)}
	s.logOverloaded(hostile)

	out := buf.String()
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("log emitted %d newlines, want 1 (a forged line was injected):\n%s", n, out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Fatalf("log carried a raw terminal escape:\n%q", out)
	}
	if !strings.Contains(out, "evil") {
		t.Fatalf("log dropped the name entirely, which defeats the point:\n%s", out)
	}
}

// --- record size limits ---
