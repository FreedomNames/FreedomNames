package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/content"
)

// Config holds runtime configuration. Values come from environment variables so
// nothing operational is hardcoded; sensible defaults keep
// `go run ./cmd/freedom-names` working.
type Config struct {
	HTTPAddr    string   // address for the HTTP API (default "127.0.0.1:8420")
	DNSAddr     string   // address for the DNS server (default ":8053")
	UpstreamDNS string   // upstream resolver for non-.fn queries (default "1.1.1.1:53")
	Bootstrap   []string // bootstrap peer multiaddrs
	ContentDir  string   // content-addressed blobstore directory

	// DNSRecursionAny lets ANY client use this node to forward non-.fn queries
	// upstream. Off by default: forwarding for the whole internet is an open
	// resolver, i.e. a reflection/amplification tool. See forwardingAllowed.
	DNSRecursionAny bool

	// HTTPAllowedHosts is the extra Host header values the HTTP API accepts
	// beyond localhost and bare IP literals. Empty by default; needed only when
	// the API is reached through a hostname (see hostAllowed).
	HTTPAllowedHosts []string

	// BootstrapMode reports whether this process runs as a bootstrap node
	// (`freedom-names bootstrap`): fixed p2p ports, DHT server mode, an HTTP
	// API on 8430 and no DNS server. Resolved once in main() and read
	// everywhere else, so the `bootstrap` positional is parsed in exactly one
	// place. Not to be confused with Bootstrap above, which is the list of
	// peers *this* node dials to join the network.
	BootstrapMode bool

	// BCH name registry for globally-unique bare names.
	BCHElectrum []string // electrum servers, tried in order with failover (empty disables bare names)
	BCHNetwork  string   // "mainnet" | "chipnet" | "testnet4" | "testnet3"
	BCHMinConf  int64    // confirmations required for a claim to count

	// Content replication and hosting policy. Content is distributed by
	// design: a publish pushes copies to other nodes, and every holder tops
	// the replica count back up — availability never rests on one node.
	ContentReplicas     int           // pushed copies per publish (target holders = replicas+1)
	ContentHostBudget   int64         // max bytes of hosted (other people's) content
	ContentHostTTL      time.Duration // hosted content expires this long after last access/re-push
	ContentHealInterval time.Duration // how often to check + top up replica counts
	ContentUpRate       int64         // bytes/s serving + pushing content (0 = unlimited)
	ContentDownRate     int64         // bytes/s fetching + receiving pushes (0 = unlimited)
	ContentMaxPushSize  int64         // largest pushed content set this node accepts
}

// Default bootstrap peers. Replace/extend with real public /dnsaddr entries as
// the network grows. Overridable via FREEDOM_BOOTSTRAP (comma-separated).
var defaultBootstrapPeers = []string{
	// "/dnsaddr/bootstrap.freedom-names.example/p2p/12D3Koo...",
}

// defaultHTTPAddr is the HTTP API address a node uses when FREEDOM_HTTP_ADDR is
// unset. A bootstrap node defaults to a different port so it can run alongside a
// normal node (notably one spawned by LibreWeb) without either failing to bind.
// Both remain overridable via FREEDOM_HTTP_ADDR / --http-addr.
func defaultHTTPAddr(bootstrapMode bool) string {
	if bootstrapMode {
		return "127.0.0.1:8430"
	}
	return "127.0.0.1:8420"
}

// LoadConfig reads configuration from the environment with defaults, for a
// normal (non-bootstrap) node. CLI paths that never run a node use this.
func LoadConfig() *Config {
	return LoadConfigForRole(false)
}

// LoadConfigForRole reads configuration from the environment with defaults,
// choosing role-dependent defaults for a bootstrap node.
//
// The role must be passed in rather than patched onto the returned Config:
// envOr cannot distinguish "unset" from "set to the default value", so
// overwriting HTTPAddr afterwards would silently clobber an explicit
// FREEDOM_HTTP_ADDR. Passing the fallback in leaves the env -> flag precedence
// chain (see applyNodeFlags) intact.
func LoadConfigForRole(bootstrapMode bool) *Config {
	cfg := &Config{
		BootstrapMode: bootstrapMode,
		// Bind the HTTP API to loopback by default: it is an unauthenticated
		// local control surface (a browser spawns the node), so it must not be
		// exposed on all interfaces. Override with FREEDOM_HTTP_ADDR=:8420 to
		// share it on a LAN deliberately.
		HTTPAddr: envOr("FREEDOM_HTTP_ADDR", defaultHTTPAddr(bootstrapMode)),
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
	// Recursion for remote clients is opt-in and spelled out explicitly, so it
	// can never be enabled by a typo in an unrelated variable.
	cfg.DNSRecursionAny = strings.EqualFold(os.Getenv("FREEDOM_DNS_RECURSION"), "any")
	if v := os.Getenv("FREEDOM_HTTP_ALLOWED_HOSTS"); v != "" {
		cfg.HTTPAllowedHosts = splitAndTrim(strings.ToLower(v))
	}
	if v := os.Getenv("FREEDOM_BCH_MINCONF"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			cfg.BCHMinConf = n
		}
	}

	// Content replication knobs. Defaults favor a robust network: 3 pushed
	// replicas per publish and a 20 GiB hosting budget per node; bandwidth is
	// unlimited unless the operator opts into a cap.
	cfg.ContentReplicas = envInt("FREEDOM_CONTENT_REPLICAS", 3)
	cfg.ContentHostBudget = envSize("FREEDOM_CONTENT_HOST_BUDGET", 20<<30)
	cfg.ContentHostTTL = envDuration("FREEDOM_CONTENT_HOST_TTL", 30*24*time.Hour)
	cfg.ContentHealInterval = envDuration("FREEDOM_CONTENT_HEAL_INTERVAL", time.Hour)
	cfg.ContentUpRate = envSize("FREEDOM_CONTENT_UP_RATE", 0)
	cfg.ContentDownRate = envSize("FREEDOM_CONTENT_DOWN_RATE", 0)
	cfg.ContentMaxPushSize = envSize("FREEDOM_CONTENT_MAX_PUSH_SIZE", content.MaxContentSize)
	return cfg
}

// parseSize parses a byte quantity: a plain integer, or an integer/decimal with
// a K/M/G/T suffix (optionally followed by "B" or "iB"), 1024-based. Examples:
// "20GB", "512MiB", "1024", "1.5G".
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	num := strings.ToUpper(s)
	num = strings.TrimSuffix(num, "IB")
	num = strings.TrimSuffix(num, "B")
	var mult int64 = 1
	switch {
	case strings.HasSuffix(num, "K"):
		mult, num = 1<<10, num[:len(num)-1]
	case strings.HasSuffix(num, "M"):
		mult, num = 1<<20, num[:len(num)-1]
	case strings.HasSuffix(num, "G"):
		mult, num = 1<<30, num[:len(num)-1]
	case strings.HasSuffix(num, "T"):
		mult, num = 1<<40, num[:len(num)-1]
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(f * float64(mult)), nil
}

// envSize reads a byte-quantity env var, logging and falling back on a bad
// value (matching the forgiving style of the other FREEDOM_* vars).
func envSize(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := parseSize(v)
	if err != nil {
		log.Printf("WARNING: %s=%q is not a valid size, using default", key, v)
		return fallback
	}
	return n
}

// envDuration reads a duration env var (time.ParseDuration syntax, plus a "d"
// days suffix, e.g. "30d").
func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if days, err := strconv.ParseFloat(strings.TrimSuffix(v, "d"), 64); err == nil && strings.HasSuffix(v, "d") {
		return time.Duration(days * 24 * float64(time.Hour))
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		log.Printf("WARNING: %s=%q is not a valid duration, using default", key, v)
		return fallback
	}
	return d
}

// envInt reads a non-negative integer env var.
func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Printf("WARNING: %s=%q is not a valid integer, using default", key, v)
		return fallback
	}
	return n
}

// Built-in Electrum/Fulcrum bootstrap servers, one list per BCH network. The
// client (internal/bch) tries them in order and fails over, so a single dead
// server never takes the registry down, fitting for a decentralized namespace.
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
// network. An unknown network yields an empty list (registry disabled) rather
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
	dir, err := content.DefaultContentDir()
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
