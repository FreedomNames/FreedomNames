package main

import (
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration. Values come from environment variables so
// nothing operational is hardcoded; sensible defaults keep `go run .` working.
type Config struct {
	HTTPAddr    string   // address for the HTTP API (default ":8420")
	DNSAddr     string   // address for the DNS server (default ":53")
	UpstreamDNS string   // upstream resolver for non-.fn queries (default "1.1.1.1:53")
	Bootstrap   []string // bootstrap peer multiaddrs

	// Layer 2 (BCH registry for globally-unique bare names).
	BCHElectrum string // electrum server, e.g. "ssl://host:50002" (empty disables L2)
	BCHNetwork  string // "chipnet" | "mainnet"
	BCHMinConf  int64  // confirmations required for a claim to count
}

// Default bootstrap peers. Replace/extend with real public /dnsaddr entries as
// the network grows. Overridable via FREEDOM_BOOTSTRAP (comma-separated).
var defaultBootstrapPeers = []string{
	// "/dnsaddr/bootstrap.freedom-names.example/p2p/12D3Koo...",
}

// LoadConfig reads configuration from the environment with defaults.
func LoadConfig() *Config {
	cfg := &Config{
		HTTPAddr: envOr("FREEDOM_HTTP_ADDR", ":8420"),
		// Default to the high port :8053 so nodes run without root. (We avoid
		// :5353, which collides with mDNS/avahi on most desktops.) Set
		// FREEDOM_DNS_ADDR=:53 (with setcap or a :53->:8053 forwarder) for
		// system-wide resolution. See the README.
		DNSAddr:     envOr("FREEDOM_DNS_ADDR", ":8053"),
		UpstreamDNS: envOr("FREEDOM_UPSTREAM_DNS", "1.1.1.1:53"),
		Bootstrap:   defaultBootstrapPeers,

		BCHNetwork:  envOr("FREEDOM_BCH_NETWORK", "chipnet"),
		BCHElectrum: envOr("FREEDOM_BCH_ELECTRUM", defaultBCHElectrum),
		BCHMinConf:  1,
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

// defaultBCHElectrum is a public chipnet Fulcrum server used when
// FREEDOM_BCH_ELECTRUM is unset. Users can point at their own server. (Verify
// this endpoint is live during chipnet testing; swap if needed.)
const defaultBCHElectrum = "ssl://chipnet.bch.ninja:50002"

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
