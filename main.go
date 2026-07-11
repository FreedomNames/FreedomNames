package main

import (
	"context"
	"os"
)

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
	dnsServer := NewDNSServer(cfg.DNSAddr, cfg.UpstreamDNS, resolver)
	if err := dnsServer.Start(); err != nil {
		panic(err)
	}
	defer dnsServer.Shutdown()

	// StartHTTPServer blocks until interrupted.
	StartHTTPServer(freedomDht, resolver, cache, cfg.HTTPAddr)
}
