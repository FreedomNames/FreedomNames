package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"syscall"
)

// isPrivilegedPortErr reports whether err is a permission-denied bind, which for
// a DNS server almost always means the configured port is privileged (<1024).
func isPrivilegedPortErr(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied")
}

// isAddrInUseErr reports whether err is an "address already in use" bind failure.
func isAddrInUseErr(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use")
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

	// The BCH registry (Layer 2) resolves globally-unique bare names via Bitcoin
	// Cash. When no electrum endpoint is configured it is left off, and bare
	// names simply resolve to not-found; self-certifying names always work.
	resolver := NewResolver(freedomDht, cache)
	if cfg.BCHElectrum != "" {
		bchClient := newElectrumClient(cfg.BCHElectrum)
		defer bchClient.Close()
		resolver = resolver.WithRegistry(NewBCHRegistry(bchClient, cfg.BCHMinConf))
		log.Printf("BCH registry enabled (%s via %s)", cfg.BCHNetwork, cfg.BCHElectrum)
	}

	// Start the DNS server (resolves .fn, forwards everything else upstream).
	// A bind failure (e.g. :53 needs privileges) is non-fatal: the DHT and HTTP
	// API are the core, and DNS is one optional resolution surface.
	dnsServer := NewDNSServer(cfg.DNSAddr, cfg.UpstreamDNS, resolver)
	if err := dnsServer.Start(); err != nil {
		log.Printf("WARNING: DNS server disabled (DHT and HTTP API still running): %v", err)
		switch {
		case isPrivilegedPortErr(err):
			log.Printf("  %s is a privileged port. Use the default high port, or", cfg.DNSAddr)
			log.Printf("  grant the capability once: sudo setcap cap_net_bind_service=+ep ./freedom-names")
		case isAddrInUseErr(err):
			log.Printf("  %s is already in use. Set FREEDOM_DNS_ADDR to a free port, e.g. FREEDOM_DNS_ADDR=:8054", cfg.DNSAddr)
		}
	} else {
		defer dnsServer.Shutdown()
	}

	// StartHTTPServer blocks until interrupted.
	StartHTTPServer(freedomDht, resolver, cache, cfg.HTTPAddr)
}
