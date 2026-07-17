package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"syscall"
)

// nodeVersion identifies this build in /health and /info so a spawning host
// (e.g. LibreWeb) can confirm it launched the expected node. It falls back to
// this compiled-in default and is overridden at release build time via
// -ldflags "-X ...main.buildVersion=<tag>" (see scripts/build-release.sh).
const defaultNodeVersion = "0.3.0"

// buildVersion is injected by the release build via -ldflags. Empty in a plain
// `go build`, in which case nodeVersion falls back to defaultNodeVersion.
var buildVersion string

// nodeVersion is the effective version string reported by the node.
var nodeVersion = func() string {
	if buildVersion != "" {
		return buildVersion
	}
	return defaultNodeVersion
}()

// applyNodeFlags lets a spawning host (e.g. LibreWeb) override config via flags,
// which take precedence over environment variables:
//
//	--http-addr HOST:PORT   full HTTP API listen address
//	--api-bind  HOST        just the bind host of the HTTP API (port unchanged)
//	--content-dir DIR       content-addressed blobstore directory
//	--dns-addr HOST:PORT    DNS server listen address
//
// Unknown flags are ignored (the only bare positional the node understands is
// "bootstrap", handled in node.go).
func applyNodeFlags(cfg *Config, args []string) {
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			break
		}
		val := args[i+1]
		switch args[i] {
		case "--http-addr":
			cfg.HTTPAddr = val
			i++
		case "--api-bind":
			_, port, ok := strings.Cut(cfg.HTTPAddr, ":")
			if !ok {
				port = "8420"
			}
			cfg.HTTPAddr = val + ":" + port
			i++
		case "--content-dir":
			cfg.ContentDir = val
			i++
		case "--dns-addr":
			cfg.DNSAddr = val
			i++
		}
	}
}

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
	applyNodeFlags(cfg, os.Args[1:]) // flags override env for a spawned node

	freedomDht := NewNode(ctx, cfg)
	defer freedomDht.Shutdown()

	cache, err := NewMemoryCache()
	if err != nil {
		panic(err)
	}

	// Attach the content service (the page-bytes layer). A failure here is
	// non-fatal: naming still works, the node just can't serve/fetch content.
	var content *ContentService
	if store, err := NewBlobStore(cfg.ContentDir); err != nil {
		log.Printf("WARNING: content service disabled: %v", err)
	} else {
		content = freedomDht.AttachContent(store, cfg)
		log.Printf("Content store at %s", cfg.ContentDir)
	}

	// The BCH registry (Layer 2) resolves globally-unique bare names via Bitcoin
	// Cash. When no electrum endpoint is configured it is left off, and bare
	// names simply resolve to not-found; self-certifying names always work.
	resolver := NewResolver(freedomDht, cache)
	if len(cfg.BCHElectrum) > 0 {
		bchClient := newElectrumClient(cfg.BCHElectrum...)
		defer bchClient.Close()
		resolver = resolver.WithRegistry(NewBCHRegistry(bchClient, cfg.BCHMinConf))
		log.Printf("BCH registry enabled (%s via %d electrum server(s), starting with %s)",
			cfg.BCHNetwork, len(cfg.BCHElectrum), cfg.BCHElectrum[0])
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
	StartHTTPServer(freedomDht, resolver, cache, content, cfg.HTTPAddr)
}
