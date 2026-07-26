package dnsserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"codeberg.org/miekg/dns/rdata"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/resolver"
)

// dnsResolveTimeout bounds a .fn lookup on the DNS path. Stub resolvers
// typically give up after ~5s, so answering later than that is wasted work.
const dnsResolveTimeout = 4 * time.Second

// DNSServer answers queries for the ".fn" zone from the DHT and forwards all
// other queries to an upstream resolver, so it can be used as a drop-in system
// resolver.
type DNSServer struct {
	resolver *resolver.Resolver
	upstream string // host:port of the upstream resolver for non-.fn queries
	// recurseAny lifts the local-client restriction on forwarding. Off by
	// default: see forwardingAllowed.
	recurseAny bool
	// inflight bounds concurrent .fn lookups; see maxInflightFN.
	inflight chan struct{}
	// dropped counts shed lookups; lastDropLog is the unix-nano timestamp of
	// the last warning, so the warning itself stays rate limited.
	dropped     atomic.Int64
	lastDropLog atomic.Int64
	udp         *dns.Server
	tcp         *dns.Server
}

// maxInflightFN bounds how many .fn queries may be walking the DHT at once.
// Each miss is a full Kademlia walk held open for dnsResolveTimeout, and the
// server answers whoever can reach the listen address, so without a ceiling a
// stream of queries for names that do not exist pins an unbounded number of
// goroutines and DHT lookups. Queries over the limit get SERVFAIL, which stub
// resolvers retry, rather than queueing behind a timeout they have outlived.
const maxInflightFN = 64

// NewDNSServer builds a DNS server listening on addr (e.g. ":53") that resolves
// ".fn" via the given resolver and forwards everything else to upstream
// (e.g. "1.1.1.1:53"). recurseAny opts into forwarding for remote clients; see
// forwardingAllowed for why that is not the default.
func NewDNSServer(addr, upstream string, resolver *resolver.Resolver, recurseAny bool) *DNSServer {
	s := &DNSServer{
		resolver:   resolver,
		upstream:   upstream,
		recurseAny: recurseAny,
		inflight:   make(chan struct{}, maxInflightFN),
	}

	mux := dns.NewServeMux()
	mux.HandleFunc("fn.", s.handleFN)
	mux.HandleFunc(".", s.handleForward)

	s.udp = &dns.Server{Addr: addr, Net: "udp", Handler: mux}
	s.tcp = &dns.Server{Addr: addr, Net: "tcp", Handler: mux}
	return s
}

// Start begins listening on UDP and TCP. The listeners are created synchronously
// and Start waits (via NotifyStartedFunc) until both servers have finished their
// internal setup before returning, so the server is ready — and safe to Shutdown
// — once Start returns.
func (s *DNSServer) Start() error {
	pc, err := net.ListenPacket("udp", s.udp.Addr)
	if err != nil {
		return fmt.Errorf("listen udp %s: %w", s.udp.Addr, err)
	}
	ln, err := net.Listen("tcp", s.tcp.Addr)
	if err != nil {
		pc.Close()
		return fmt.Errorf("listen tcp %s: %w", s.tcp.Addr, err)
	}
	s.udp.PacketConn = pc
	s.tcp.Listener = ln

	udpReady := make(chan struct{})
	tcpReady := make(chan struct{})
	s.udp.NotifyStartedFunc = func(context.Context) { close(udpReady) }
	s.tcp.NotifyStartedFunc = func(context.Context) { close(tcpReady) }

	go func() {
		if err := s.udp.ListenAndServe(); err != nil {
			log.Printf("DNS UDP server error: %v", err)
		}
	}()
	go func() {
		if err := s.tcp.ListenAndServe(); err != nil {
			log.Printf("DNS TCP server error: %v", err)
		}
	}()

	// Wait for both servers to finish init() and begin serving before returning.
	<-udpReady
	<-tcpReady

	log.Printf("DNS server listening on %s (udp+tcp), forwarding to %s", s.udp.Addr, s.upstream)
	return nil
}

// Shutdown stops both listeners.
func (s *DNSServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.udp.Shutdown(ctx)
	s.tcp.Shutdown(ctx)
}

// handleFN answers queries for the ".fn" zone from the DHT.
func (s *DNSServer) handleFN(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) {
	if err := r.Unpack(); err != nil {
		log.Printf("DNS: unpack: %v", err)
		return
	}
	if len(r.Question) == 0 {
		return
	}
	q := r.Question[0]
	name := q.Header().Name
	qtype := dns.RRToType(q)

	// Take a slot before starting a DHT walk, so a flood of queries cannot pin
	// an unbounded number of concurrent lookups.
	select {
	case s.inflight <- struct{}{}:
		defer func() { <-s.inflight }()
	default:
		// Throttled: the cap is reached precisely when queries are flooding in,
		// and a line per refused query would turn a lookup flood into a log
		// flood.
		s.logOverloaded(name)
		r.Reset()
		r.Response = true
		r.RecursionAvailable = true
		r.Rcode = dns.RcodeServerFailure
		respond(w, r)
		return
	}

	// Bound the lookup below a typical stub-resolver patience (~5s): a slow
	// DHT walk should fail fast here rather than answer a client that already
	// gave up.
	resolveCtx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
	defer cancel()
	records, err := s.resolver.Resolve(resolveCtx, name)

	// Re-use r as the response.
	r.Reset()
	r.Response = true
	r.RecursionAvailable = true

	if err != nil {
		// %q, not %s: a query name is raw bytes off the wire. The DNS library
		// escapes embedded dots and nothing else, so a label may carry newlines
		// or terminal escapes, and answering .fn for anyone who asks is the
		// design — an unauthenticated packet must not be able to write forged
		// lines into this node's log.
		log.Printf("DNS: resolve %q: %v", name, err)
		if errors.Is(err, context.DeadlineExceeded) {
			r.Rcode = dns.RcodeServerFailure // transient: lookup timed out
		} else {
			r.Rcode = dns.RcodeNameError // NXDOMAIN
		}
		respond(w, r)
		return
	}

	for _, rr := range records {
		if answer := toDNSRR(name, rr, qtype); answer != nil {
			r.Answer = append(r.Answer, answer)
		}
	}
	respond(w, r)
}

// handleForward proxies non-.fn queries to the configured upstream resolver,
// but only for clients allowed to use this node recursively.
func (s *DNSServer) handleForward(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) {
	if err := r.Unpack(); err != nil {
		log.Printf("DNS: unpack: %v", err)
		return
	}
	if !s.recurseAny && !forwardingAllowed(w.RemoteAddr()) {
		r.Reset()
		r.Response = true
		r.Rcode = dns.RcodeRefused
		respond(w, r)
		return
	}
	resp, err := dns.Exchange(ctx, r, "udp", s.upstream)
	if err != nil || resp == nil {
		log.Printf("DNS: forward to %s failed: %v", s.upstream, err)
		r.Reset()
		r.Response = true
		r.Rcode = dns.RcodeServerFailure
		respond(w, r)
		return
	}
	respond(w, resp)
}

// overloadLogInterval is the minimum gap between "lookups in flight" warnings.
const overloadLogInterval = 10 * time.Second

// logOverloaded reports that a lookup was shed, at most once per
// overloadLogInterval, counting the ones it swallowed so the log still conveys
// the scale.
func (s *DNSServer) logOverloaded(name string) {
	dropped := s.dropped.Add(1)
	last := s.lastDropLog.Load()
	now := time.Now().UnixNano()
	if now-last < int64(overloadLogInterval) || !s.lastDropLog.CompareAndSwap(last, now) {
		return
	}
	log.Printf("DNS: shedding lookups (%d refused so far, %d already in flight), most recently %q",
		dropped, maxInflightFN, name)
}

// forwardingAllowed reports whether a client may use this node as a recursive
// resolver. Answering ".fn" is authoritative data anyone may ask for, but
// forwarding *arbitrary* queries upstream for *anyone* makes the node an open
// resolver — a reflection/amplification tool pointed at third parties. The
// default listen address covers every interface, so recursion is restricted to
// the clients the documented setups actually use: this machine and the local
// network. Operators who deliberately run a public forwarder can set
// FREEDOM_DNS_RECURSION=any.
func forwardingAllowed(addr net.Addr) bool {
	var ip net.IP
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
	case *net.TCPAddr:
		ip = a.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
	}
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// respond packs a message and writes it. Pack leaves a partially-filled buffer
// behind on failure (e.g. a record too large to represent on the wire) and the
// writer would happily put those bytes on the wire, so a failure is turned into
// an empty SERVFAIL instead of a malformed answer.
func respond(w dns.ResponseWriter, m *dns.Msg) {
	if err := m.Pack(); err != nil {
		log.Printf("DNS: pack response: %v", err)
		m.Reset()
		m.Response = true
		m.Rcode = dns.RcodeServerFailure
		if err := m.Pack(); err != nil {
			return
		}
	}
	if _, err := io.Copy(w, m); err != nil {
		log.Printf("DNS: write response: %v", err)
	}
}

// toDNSRR converts a Freedom Names record.RR into a wire DNS record.RR for the given query
// type. It returns nil if the record does not answer the query type.
func toDNSRR(name string, rr record.RR, qtype uint16) dns.RR {
	hdr := dns.Header{Name: name, TTL: rr.TTL, Class: dns.ClassINET}
	switch rr.Type {
	case record.RecordTypeA:
		if qtype != dns.TypeA {
			return nil
		}
		addr, err := netip.ParseAddr(rr.Value)
		if err != nil {
			return nil
		}
		// Unmap accepts the IPv4-mapped IPv6 form ("::ffff:1.2.3.4") that
		// record validation also accepts, normalizing it to plain IPv4.
		addr = addr.Unmap()
		if !addr.Is4() {
			return nil
		}
		return &dns.A{Hdr: hdr, A: rdata.A{Addr: addr}}
	case record.RecordTypeAAAA:
		if qtype != dns.TypeAAAA {
			return nil
		}
		addr, err := netip.ParseAddr(rr.Value)
		if err != nil || !addr.Is6() {
			return nil
		}
		return &dns.AAAA{Hdr: hdr, AAAA: rdata.AAAA{Addr: addr}}
	case record.RecordTypeTXT:
		if qtype != dns.TypeTXT {
			return nil
		}
		return &dns.TXT{Hdr: hdr, TXT: rdata.TXT{Txt: []string{rr.Value}}}
	case record.RecordTypeCNAME:
		// Per RFC 1034 §3.6.2 a CNAME answers queries for other types too:
		// return the CNAME for A/AAAA (and CNAME) queries so CNAME-only names
		// stay reachable through normal clients, which chase the target.
		if qtype != dns.TypeCNAME && qtype != dns.TypeA && qtype != dns.TypeAAAA {
			return nil
		}
		return &dns.CNAME{Hdr: hdr, CNAME: rdata.CNAME{Target: dnsutil.Fqdn(rr.Value)}}
	default:
		return nil
	}
}
