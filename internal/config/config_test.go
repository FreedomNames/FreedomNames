package config

import (
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"1024", 1024, true},
		{"20GB", 20 << 30, true},
		{"20G", 20 << 30, true},
		{"512MiB", 512 << 20, true},
		{"1.5G", 3 << 29, true},
		{"64K", 64 << 10, true},
		{"1T", 1 << 40, true},
		{"0", 0, true},
		{" 10MB ", 10 << 20, true},
		{"", 0, false},
		{"lots", 0, false},
		{"-5G", 0, false},
	}
	for _, tc := range cases {
		got, err := parseSize(tc.in)
		if tc.ok != (err == nil) {
			t.Errorf("parseSize(%q): err=%v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("FN_TEST_SIZE", "1GB")
	if got := envSize("FN_TEST_SIZE", 5); got != 1<<30 {
		t.Errorf("envSize set = %d", got)
	}
	if got := envSize("FN_TEST_SIZE_UNSET", 5); got != 5 {
		t.Errorf("envSize fallback = %d", got)
	}
	t.Setenv("FN_TEST_SIZE_BAD", "banana")
	if got := envSize("FN_TEST_SIZE_BAD", 5); got != 5 {
		t.Errorf("envSize bad value = %d, want fallback", got)
	}

	t.Setenv("FN_TEST_DUR", "90m")
	if got := envDuration("FN_TEST_DUR", time.Hour); got != 90*time.Minute {
		t.Errorf("envDuration = %v", got)
	}
	t.Setenv("FN_TEST_DUR_DAYS", "30d")
	if got := envDuration("FN_TEST_DUR_DAYS", time.Hour); got != 30*24*time.Hour {
		t.Errorf("envDuration days = %v", got)
	}
	if got := envDuration("FN_TEST_DUR_UNSET", time.Hour); got != time.Hour {
		t.Errorf("envDuration fallback = %v", got)
	}

	t.Setenv("FN_TEST_INT", "7")
	if got := envInt("FN_TEST_INT", 3); got != 7 {
		t.Errorf("envInt = %d", got)
	}
	t.Setenv("FN_TEST_INT_BAD", "-2")
	if got := envInt("FN_TEST_INT_BAD", 3); got != 3 {
		t.Errorf("envInt negative = %d, want fallback", got)
	}
}

// TestDefaultBCHElectrumServers checks each known network selects a non-empty
// list and an unknown one disables the registry (empty) rather than guessing.
func TestDefaultBCHElectrumServers(t *testing.T) {
	for _, network := range []string{"mainnet", "chipnet", "testnet4", "testnet3"} {
		if got := defaultBCHElectrumServers(network); len(got) == 0 {
			t.Errorf("network %q: expected built-in servers, got none", network)
		}
	}
	if got := defaultBCHElectrumServers("regtest"); got != nil {
		t.Errorf("unknown network: expected nil (registry disabled), got %v", got)
	}
}

// TestHTTPAddrDefaultByRole pins the role-dependent HTTP API default. A
// bootstrap node must not land on 8420, so it can run alongside a normal node
// (notably one spawned by LibreWeb) without either failing to bind.
func TestHTTPAddrDefaultByRole(t *testing.T) {
	// Ensure the env does not leak in from the developer's shell.
	t.Setenv("FREEDOM_HTTP_ADDR", "")
	t.Setenv("FREEDOM_AUTHORING_ADDR", "")

	cases := []struct {
		name          string
		bootstrapMode bool
		want          string
	}{
		{"normal node keeps 8420", false, "127.0.0.1:8420"},
		{"bootstrap node uses 8430", true, "127.0.0.1:8430"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := LoadConfigForRole(tc.bootstrapMode)
			if cfg.HTTPAddr != tc.want {
				t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, tc.want)
			}
			if cfg.BootstrapMode != tc.bootstrapMode {
				t.Errorf("BootstrapMode = %v, want %v", cfg.BootstrapMode, tc.bootstrapMode)
			}
			if cfg.AuthoringAddr != "127.0.0.1:8421" {
				t.Errorf("AuthoringAddr = %q, want 127.0.0.1:8421", cfg.AuthoringAddr)
			}
		})
	}

	// LoadConfig is the normal-node case, for CLI paths that never run a node.
	if cfg := LoadConfig(); cfg.HTTPAddr != "127.0.0.1:8420" || cfg.BootstrapMode {
		t.Errorf("LoadConfig() = {HTTPAddr:%q BootstrapMode:%v}, want the normal-node defaults",
			cfg.HTTPAddr, cfg.BootstrapMode)
	}
}
