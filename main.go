package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
)

// isPrivilegedPortErr reports whether err is a permission-denied bind, which for
// a DNS server almost always means the configured port is privileged (<1024).
func isPrivilegedPortErr(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied")
}

func main() {
	// A "freedom" subcommand invokes the CLI instead of running a node.
	if len(os.Args) > 1 && os.Args[1] == "freedom" {
		RunCLI(os.Args[2:])
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := LoadConfig()

	freedomDht := NewNode(ctx, cfg)
	defer freedomDht.Shutdown()

	cache, err := NewMemoryCache()
	if err != nil {
		panic(err)
	}

	// The BCH registry (Layer 2) resolves bare names; it is a stub today, so bare
	// names return not-implemented and self-certifying names resolve as normal.
	resolver := NewResolver(freedomDht, cache).WithRegistry(NewBCHRegistry())

	// Start the DNS server (resolves .fn, forwards everything else upstream).
	// A bind failure (e.g. :53 needs privileges) is non-fatal: the DHT and HTTP
	// API are the core, and DNS is one optional resolution surface.
	dnsServer := NewDNSServer(cfg.DNSAddr, cfg.UpstreamDNS, resolver)
	if err := dnsServer.Start(); err != nil {
		log.Printf("WARNING: DNS server disabled: %v", err)
		if isPrivilegedPortErr(err) {
			log.Printf("  %s is a privileged port. Use a high port, e.g. FREEDOM_DNS_ADDR=127.0.0.1:15353,", cfg.DNSAddr)
			log.Printf("  or grant the capability once: sudo setcap cap_net_bind_service=+ep ./freedom-names")
		}
	} else {
		defer dnsServer.Shutdown()
	}

	// StartHTTPServer blocks until interrupted.
	StartHTTPServer(freedomDht, resolver, cache, cfg.HTTPAddr)
}
