// Command freedom-names runs a Freedom Names node: a libp2p DHT peer that
// resolves .fn names, optionally serving DNS and a local HTTP API, and hosting
// content. It is also the entry point for the `freedom` management subcommands.
//
// This file is wiring only — every moving part lives under internal/.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/bch"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/bind"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/cli"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/config"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/content"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/dnsserver"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/httpapi"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/node"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/resolver"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/version"
)

// applyNodeFlags lets a spawning host (e.g. LibreWeb) override config via flags,
// which take precedence over environment variables:
//
//	--http-addr HOST:PORT   full HTTP API listen address
//	--authoring-addr HOST:PORT  loopback-only authoring API listen address
//	--api-bind  HOST        just the bind host of the HTTP API (port unchanged)
//	--content-dir DIR       content-addressed blobstore directory
//	--dns-addr HOST:PORT    DNS server listen address
//
// Unknown flags are ignored (the only bare positional the node understands is
// "bootstrap", handled in main).
func applyNodeFlags(cfg *config.Config, args []string) {
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			break
		}
		val := args[i+1]
		switch args[i] {
		case "--http-addr":
			cfg.HTTPAddr = val
			i++
		case "--authoring-addr":
			cfg.AuthoringAddr = val
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

// nodeUsage documents the node binary itself; the `freedom` subcommand has
// its own usage in internal/cli.
const nodeUsage = `freedom-names - decentralized naming (.fn) node

Usage:
  freedom-names [flags]            Run a node (DHT peer + DNS + HTTP API)
  freedom-names bootstrap          Run a bootstrap node (fixed p2p ports, DHT
                                   server mode, HTTP API on :8430, no DNS)
  freedom-names freedom <command>  Manage names (see: freedom-names freedom help)

Flags:
  --http-addr HOST:PORT   HTTP API listen address (default 127.0.0.1:8420)
  --authoring-addr HOST:PORT  Owner-key API (loopback only, default 127.0.0.1:8421)
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
			cli.RunCLI(os.Args[2:])
			return
		case "-h", "--help", "help":
			fmt.Print(nodeUsage)
			return
		case "--version":
			fmt.Printf("freedom-names %s\n", version.String())
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

	cfg := config.LoadConfigForRole(bootstrapMode)
	applyNodeFlags(cfg, os.Args[1:]) // flags override env for a spawned node

	freedomDht := node.NewNode(ctx, cfg)
	defer freedomDht.Shutdown()

	cache, err := resolver.NewMemoryCache()
	if err != nil {
		panic(err)
	}

	// Attach the content service (the page-bytes layer). A failure here is
	// non-fatal: naming still works, the node just can't serve/fetch content.
	var contentSvc *node.ContentService
	if store, err := content.NewBlobStore(cfg.ContentDir); err != nil {
		log.Printf("WARNING: content service disabled: %v", err)
	} else {
		contentSvc = freedomDht.AttachContent(store, cfg)
		log.Printf("Content store at %s", cfg.ContentDir)
	}

	// The BCH name registry resolves globally-unique bare names via Bitcoin
	// Cash. When no electrum endpoint is configured it is left off, and bare
	// names simply resolve to not-found; self-certifying names always work.
	res := resolver.NewResolver(freedomDht, cache)
	if len(cfg.BCHElectrum) > 0 {
		bchClient := bch.NewElectrumClient(cfg.BCHElectrum...)
		defer bchClient.Close()
		res = res.WithRegistry(bch.NewBCHRegistry(bchClient, cfg.BCHMinConf))
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
		dnsServer := dnsserver.NewDNSServer(cfg.DNSAddr, cfg.UpstreamDNS, res, cfg.DNSRecursionAny)
		if cfg.DNSRecursionAny {
			log.Printf("WARNING: FREEDOM_DNS_RECURSION=any - this node forwards queries for ANY client (open resolver)")
		}
		if err := dnsServer.Start(); err != nil {
			log.Printf("WARNING: DNS server disabled (DHT and HTTP API still running): %v", err)
			switch {
			case bind.IsPrivilegedPort(err):
				log.Printf("  %s is a privileged port. Use the default high port, or", cfg.DNSAddr)
				log.Printf("  grant the capability once: sudo setcap cap_net_bind_service=+ep ./freedom-names")
			case bind.IsAddrInUse(err):
				log.Printf("  %s is already in use. Set FREEDOM_DNS_ADDR to a free port, e.g. FREEDOM_DNS_ADDR=:8054", cfg.DNSAddr)
			}
		} else {
			defer dnsServer.Shutdown()
		}
	}

	// StartHTTPServer blocks until interrupted.
	httpapi.StartHTTPServer(freedomDht, res, cache, contentSvc, cfg.HTTPAddr, cfg.AuthoringAddr, cfg.BootstrapMode, cfg.HTTPAllowedHosts)
}
