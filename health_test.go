package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	kbucket "github.com/libp2p/go-libp2p-kbucket"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// stubDHT is a FreedomDHT whose only meaningful behaviour is IsInitialized.
// /health reports liveness and role without touching the DHT otherwise, so
// every other method is an unused no-op here.
type stubDHT struct{ initialized bool }

func (s stubDHT) IsInitialized() bool { return s.initialized }
func (s stubDHT) Shutdown()           {}
func (s stubDHT) PutValue(string, []byte) error {
	return nil
}
func (s stubDHT) GetValue(string) ([]byte, error) { return nil, nil }
func (s stubDHT) PublishRecord(*FNRecord) error   { return nil }
func (s stubDHT) ResolveRecord(context.Context, string) (*FNRecord, error) {
	return nil, nil
}
func (s stubDHT) GetMode() string                           { return "Client" }
func (s stubDHT) GetPeerInfos() []kbucket.PeerInfo          { return nil }
func (s stubDHT) GetRoutingPeers() []peer.ID                { return nil }
func (s stubDHT) GetNetworkPeers() []peer.ID                { return nil }
func (s stubDHT) GetPeerID() string                         { return "" }
func (s stubDHT) GetListenAddresses() []multiaddr.Multiaddr { return nil }
func (s stubDHT) GetNetworkSize() (int32, error)            { return 0, nil }
func (s stubDHT) GetProtocols() []protocol.ID               { return nil }

// getHealth calls the handler and decodes its JSON body.
func getHealth(t *testing.T, dht FreedomDHT, role string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	HealthHandler(dht, role).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /health body %q: %v", rec.Body.String(), err)
	}
	return body
}

// TestHealthReportsRole verifies /health distinguishes a normal node from a
// bootstrap node. A spawning host (LibreWeb) uses this to decide whether it may
// adopt an already-running node or must start its own.
func TestHealthReportsRole(t *testing.T) {
	cases := []struct {
		name          string
		bootstrapMode bool
		want          string
	}{
		{"normal node", false, RoleNode},
		{"bootstrap node", true, RoleBootstrap},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := getHealth(t, stubDHT{initialized: true}, roleFor(tc.bootstrapMode))
			if got := body["role"]; got != tc.want {
				t.Errorf("role = %v, want %q", got, tc.want)
			}
		})
	}
}

// TestHealthRolePresentWhenNotReady is the property libreweb depends on: role
// is populated even before the DHT is initialized. /info 500s until the DHT is
// up, so a host probing a still-starting bootstrap node must still learn what
// it is -- otherwise it reads "nothing here" and double-spawns.
func TestHealthRolePresentWhenNotReady(t *testing.T) {
	body := getHealth(t, stubDHT{initialized: false}, RoleBootstrap)

	if ready, ok := body["ready"].(bool); !ok || ready {
		t.Fatalf("ready = %v, want false (precondition for this test)", body["ready"])
	}
	if got := body["role"]; got != RoleBootstrap {
		t.Errorf("role = %v, want %q while ready is false", got, RoleBootstrap)
	}
	if got := body["status"]; got != "ok" {
		t.Errorf("status = %v, want \"ok\" even when not ready", got)
	}
}
