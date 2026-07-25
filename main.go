package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
)

// nodeVersion identifies this build in /health and /info so a spawning host
// (e.g. LibreWeb) can confirm it launched the expected node. It falls back to
// this compiled-in default and is overridden at release build time via
// -ldflags "-X main.buildVersion=<tag>" (see scripts/build-release.sh).
// The fallback deliberately never looks like a release version, so an
// uninjected build can't masquerade as one.
const defaultNodeVersion = "0.0.0-dev"

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
			// SplitHostPort, not a plain Cut: an IPv6 listen address
			// ("[::1]:8420") has colons in the host too, and cutting at the
			// first one would yield a nonsense port.
			_, port, err := net.SplitHostPort(cfg.HTTPAddr)
			if err != nil {
				port = "8420"
			}
			cfg.HTTPAddr = net.JoinHostPort(val, port)
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

// nodeUsage documents the node binary itself; the `freedom` subcommand has
// its own usage in cli.go.
const nodeUsage = `freedom-names - decentralized naming (.fn) node

Usage:
  freedom-names [flags]            Run a node (DHT peer + DNS + HTTP API)
  freedom-names bootstrap          Run a bootstrap node (fixed p2p ports, DHT
                                   server mode, HTTP API on :8430, no DNS)
  freedom-names freedom <command>  Manage names (see: freedom-names freedom help)

Flags:
  --http-addr HOST:PORT   HTTP API listen address (default 127.0.0.1:8420)
  --api-bind HOST         Bind host of the HTTP API (port unchanged)
  --content-dir DIR       Content blobstore directory (default ~/.freedom/content)
  --dns-addr HOST:PORT    DNS server listen address (default :8053)
  -h, --help              Show this help
  --version               Show the node version

Configuration is otherwise driven by FREEDOM_* environment variables; flags
take precedence. Docs: https://freedomnames.org
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		// A "freedom" subcommand invokes the CLI instead of running a node.
		case "freedom":
			RunCLI(os.Args[2:])
			return
		case "-h", "--help", "help":
			fmt.Print(nodeUsage)
			return
		case "--version":
			fmt.Printf("freedom-names %s\n", nodeVersion)
			return
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Resolve the bootstrap role once, here: it is the only place the bare
	// `bootstrap` positional is parsed. It picks role-dependent config defaults
	// (so it must be known before the config load), and is then carried on cfg
	// for the DHT setup, the DNS gate and the /health + /info role field.
	bootstrapMode := len(os.Args) > 1 && os.Args[1] == "bootstrap"

	cfg := LoadConfigForRole(bootstrapMode)
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

	// The BCH name registry resolves globally-unique bare names via Bitcoin
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
	//
	// A bootstrap node runs no DNS server at all. It is a rendezvous peer for
	// others joining the network, not a resolver for local clients: nothing
	// should point a stub resolver at it, and a forwarding listener is an open
	// resolver surface that has no business on a public server.
	if cfg.BootstrapMode {
		log.Println("Bootstrap node: DNS server not started")
	} else {
		dnsServer := NewDNSServer(cfg.DNSAddr, cfg.UpstreamDNS, resolver, cfg.DNSRecursionAny)
		if cfg.DNSRecursionAny {
			log.Printf("WARNING: FREEDOM_DNS_RECURSION=any - this node forwards queries for ANY client (open resolver)")
		}
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
	}

	// StartHTTPServer blocks until interrupted.
	StartHTTPServer(freedomDht, resolver, cache, content, cfg.HTTPAddr, cfg.BootstrapMode, cfg.HTTPAllowedHosts)
}
