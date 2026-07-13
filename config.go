package main

import (
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration. Values come from environment variables so
// nothing operational is hardcoded; sensible defaults keep `go run .` working.
type Config struct {
	HTTPAddr    string   // address for the HTTP API (default "127.0.0.1:8420")
	DNSAddr     string   // address for the DNS server (default ":8053")
	UpstreamDNS string   // upstream resolver for non-.fn queries (default "1.1.1.1:53")
	Bootstrap   []string // bootstrap peer multiaddrs
	ContentDir  string   // content-addressed blobstore directory

	// Layer 2 (BCH registry for globally-unique bare names).
	BCHElectrum []string // electrum servers, tried in order with failover (empty disables L2)
	BCHNetwork  string   // "mainnet" | "chipnet" | "testnet4" | "testnet3"
	BCHMinConf  int64    // confirmations required for a claim to count
}

// Default bootstrap peers. Replace/extend with real public /dnsaddr entries as
// the network grows. Overridable via FREEDOM_BOOTSTRAP (comma-separated).
var defaultBootstrapPeers = []string{
	// "/dnsaddr/bootstrap.freedom-names.example/p2p/12D3Koo...",
}

// LoadConfig reads configuration from the environment with defaults.
func LoadConfig() *Config {
	cfg := &Config{
		// Bind the HTTP API to loopback by default: it is an unauthenticated
		// local control surface (a browser spawns the node), so it must not be
		// exposed on all interfaces. Override with FREEDOM_HTTP_ADDR=:8420 to
		// share it on a LAN deliberately.
		HTTPAddr: envOr("FREEDOM_HTTP_ADDR", "127.0.0.1:8420"),
		// Default to the high port :8053 so nodes run without root. (We avoid
		// :5353, which collides with mDNS/avahi on most desktops.) Set
		// FREEDOM_DNS_ADDR=:53 (with setcap or a :53->:8053 forwarder) for
		// system-wide resolution. See the README.
		DNSAddr:     envOr("FREEDOM_DNS_ADDR", ":8053"),
		UpstreamDNS: envOr("FREEDOM_UPSTREAM_DNS", "1.1.1.1:53"),
		Bootstrap:   defaultBootstrapPeers,
		ContentDir:  envOr("FREEDOM_CONTENT_DIR", defaultContentDirOr()),

		// Default to mainnet: bare names are a real, globally-unique namespace.
		// Point FREEDOM_BCH_NETWORK at chipnet/testnet4 to experiment with free
		// faucet coins (see the README).
		BCHNetwork: envOr("FREEDOM_BCH_NETWORK", "mainnet"),
		BCHMinConf: 1,
	}
	// Electrum servers: an explicit FREEDOM_BCH_ELECTRUM (comma-separated) wins;
	// otherwise use the built-in bootstrap list for the selected network.
	if v := os.Getenv("FREEDOM_BCH_ELECTRUM"); v != "" {
		cfg.BCHElectrum = splitAndTrim(v)
	} else {
		cfg.BCHElectrum = defaultBCHElectrumServers(cfg.BCHNetwork)
	}
	if v := os.Getenv("FREEDOM_BOOTSTRAP"); v != "" {
		cfg.Bootstrap = splitAndTrim(v)
	}
	if v := os.Getenv("FREEDOM_BCH_MINCONF"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			cfg.BCHMinConf = n
		}
	}
	return cfg
}

// Built-in Electrum/Fulcrum bootstrap servers, one list per BCH network. The
// client (electrum.go) tries them in order and fails over, so a single dead
// server never takes Layer 2 down, fitting for a decentralized namespace.
// Sourced from Electron Cash's public server lists (SSL endpoints only; .onion
// servers are omitted since we dial plain TLS, not Tor).
//
// NOTE: any public server sees which bare names you resolve. For privacy or
// guaranteed availability, run your own Fulcrum and set FREEDOM_BCH_ELECTRUM.
var (
	bchMainnetElectrum = []string{
		"ssl://bch.imaginary.cash:50002",
		"ssl://electrum.imaginary.cash:50002",
		"ssl://bch.loping.net:50002",
		"ssl://electroncash.dk:50002",
		"ssl://bch0.kister.net:50002",
		"ssl://cashnode.bch.ninja:50002",
		"ssl://fulcrum.criptolayer.net:50002",
		"ssl://blackie.c3-soft.com:50002",
	}
	bchChipnetElectrum = []string{
		"ssl://chipnet.bch.ninja:50002",
		"ssl://chipnet.imaginary.cash:50002",
		"ssl://chipnet.c3-soft.com:64002",
		"ssl://cbch.loping.net:62102",
	}
	bchTestnet4Electrum = []string{
		"ssl://tbch4.loping.net:62002",
		"ssl://blackie.c3-soft.com:62002",
	}
	bchTestnet3Electrum = []string{
		"ssl://testnet.imaginary.cash:50002",
		"ssl://tbch.loping.net:60002",
		"ssl://testnet.bitcoincash.network:60002",
		"ssl://bch0.kister.net:51002",
		"ssl://blackie.c3-soft.com:60002",
	}
)

// defaultBCHElectrumServers returns the built-in Electrum bootstrap list for a
// network. An unknown network yields an empty list (Layer 2 disabled) rather
// than silently pointing at the wrong chain.
func defaultBCHElectrumServers(network string) []string {
	switch network {
	case "mainnet":
		return bchMainnetElectrum
	case "chipnet":
		return bchChipnetElectrum
	case "testnet4":
		return bchTestnet4Electrum
	case "testnet3", "testnet":
		return bchTestnet3Electrum
	default:
		return nil
	}
}

// defaultContentDirOr returns ~/.freedom/content, or "" if the home dir can't
// be determined (the caller then reports the store as disabled).
func defaultContentDirOr() string {
	dir, err := defaultContentDir()
	if err != nil {
		return ""
	}
	return dir
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
