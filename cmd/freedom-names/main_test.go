package main

import (
	"testing"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/config"
)

// TestHTTPAddrPrecedence guards the env -> flag chain. envOr cannot tell "unset"
// from "set to the default", so a role-dependent default applied after the
// config load would silently clobber an explicit FREEDOM_HTTP_ADDR. These cases
// would catch that regression.
func TestHTTPAddrPrecedence(t *testing.T) {
	t.Run("env wins over the bootstrap default", func(t *testing.T) {
		t.Setenv("FREEDOM_HTTP_ADDR", "127.0.0.1:9999")

		if cfg := config.LoadConfigForRole(true); cfg.HTTPAddr != "127.0.0.1:9999" {
			t.Errorf("HTTPAddr = %q, want the explicit env value", cfg.HTTPAddr)
		}
	})

	t.Run("env set to the normal default still wins", func(t *testing.T) {
		// The exact case envOr cannot distinguish: a bootstrap node explicitly
		// pointed at 8420 must stay there, not be moved to 8430.
		t.Setenv("FREEDOM_HTTP_ADDR", "127.0.0.1:8420")

		if cfg := config.LoadConfigForRole(true); cfg.HTTPAddr != "127.0.0.1:8420" {
			t.Errorf("HTTPAddr = %q, want the explicit 127.0.0.1:8420", cfg.HTTPAddr)
		}
	})

	t.Run("flag wins over env and the bootstrap default", func(t *testing.T) {
		t.Setenv("FREEDOM_HTTP_ADDR", "127.0.0.1:9999")

		cfg := config.LoadConfigForRole(true)
		applyNodeFlags(cfg, []string{"bootstrap", "--http-addr", "127.0.0.1:7777"})
		if cfg.HTTPAddr != "127.0.0.1:7777" {
			t.Errorf("HTTPAddr = %q, want the flag value", cfg.HTTPAddr)
		}
	})
}

func TestAuthoringAddrFlagWinsOverEnv(t *testing.T) {
	t.Setenv("FREEDOM_AUTHORING_ADDR", "127.0.0.1:9998")
	cfg := config.LoadConfig()
	applyNodeFlags(cfg, []string{"--authoring-addr", "127.0.0.1:7778"})
	if cfg.AuthoringAddr != "127.0.0.1:7778" {
		t.Errorf("AuthoringAddr = %q, want flag value", cfg.AuthoringAddr)
	}
}
