package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"codeberg.org/miekg/dns/rdata"
)

// DNSServer answers queries for the ".fn" zone from the DHT and forwards all
// other queries to an upstream resolver, so it can be used as a drop-in system
// resolver.
type DNSServer struct {
	resolver *Resolver
	upstream string // host:port of the upstream resolver for non-.fn queries
	udp      *dns.Server
	tcp      *dns.Server
}

// NewDNSServer builds a DNS server listening on addr (e.g. ":53") that resolves
// ".fn" via the given resolver and forwards everything else to upstream
// (e.g. "1.1.1.1:53").
func NewDNSServer(addr, upstream string, resolver *Resolver) *DNSServer {
	s := &DNSServer{resolver: resolver, upstream: upstream}

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

	records, err := s.resolver.Resolve(name)

	// Re-use r as the response.
	r.Reset()
	r.Response = true
	r.RecursionAvailable = true

	if err != nil {
		log.Printf("DNS: resolve %s: %v", name, err)
		r.Rcode = dns.RcodeNameError // NXDOMAIN
		r.Pack()
		io.Copy(w, r)
		return
	}

	for _, rr := range records {
		if answer := toDNSRR(name, rr, qtype); answer != nil {
			r.Answer = append(r.Answer, answer)
		}
	}
	r.Pack()
	io.Copy(w, r)
}

// handleForward proxies non-.fn queries to the configured upstream resolver.
func (s *DNSServer) handleForward(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) {
	if err := r.Unpack(); err != nil {
		log.Printf("DNS: unpack: %v", err)
		return
	}
	resp, err := dns.Exchange(ctx, r, "udp", s.upstream)
	if err != nil || resp == nil {
		log.Printf("DNS: forward to %s failed: %v", s.upstream, err)
		r.Reset()
		r.Response = true
		r.Rcode = dns.RcodeServerFailure
		r.Pack()
		io.Copy(w, r)
		return
	}
	resp.Pack()
	io.Copy(w, resp)
}

// toDNSRR converts a Freedom Names RR into a wire DNS RR for the given query
// type. It returns nil if the record does not answer the query type.
func toDNSRR(name string, rr RR, qtype uint16) dns.RR {
	hdr := dns.Header{Name: name, TTL: rr.TTL, Class: dns.ClassINET}
	switch rr.Type {
	case RecordTypeA:
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
	case RecordTypeAAAA:
		if qtype != dns.TypeAAAA {
			return nil
		}
		addr, err := netip.ParseAddr(rr.Value)
		if err != nil || !addr.Is6() {
			return nil
		}
		return &dns.AAAA{Hdr: hdr, AAAA: rdata.AAAA{Addr: addr}}
	case RecordTypeTXT:
		if qtype != dns.TypeTXT {
			return nil
		}
		return &dns.TXT{Hdr: hdr, TXT: rdata.TXT{Txt: []string{rr.Value}}}
	case RecordTypeCNAME:
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
