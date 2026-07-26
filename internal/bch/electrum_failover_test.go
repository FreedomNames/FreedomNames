package bch

import (
	"context"
	"net"
	"testing"
)

// TestElectrumFailoverSkipsDeadEndpoint points the client at a dead endpoint
// followed by a live mock, and checks it fails over and completes a call.
func TestElectrumFailoverSkipsDeadEndpoint(t *testing.T) {
	live := newMockElectrum(t)

	// A closed listener yields an address nothing is listening on.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadEndpoint := "tcp://" + dead.Addr().String()
	dead.Close()

	client := NewElectrumClient(deadEndpoint, live.endpoint())
	defer client.Close()

	// server.version is negotiated on connect; BlockHeight forces a real call.
	if _, err := client.BlockHeight(context.Background()); err != nil {
		t.Fatalf("expected failover to the live endpoint, got error: %v", err)
	}
	if got := client.endpoints[client.lastGood]; got != live.endpoint() {
		t.Fatalf("lastGood = %q, want live endpoint %q", got, live.endpoint())
	}
}

// TestElectrumAllEndpointsDown reports an error (not a panic) when every
// endpoint is unreachable.
func TestElectrumAllEndpointsDown(t *testing.T) {
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	endpoint := "tcp://" + dead.Addr().String()
	dead.Close()

	client := NewElectrumClient(endpoint, endpoint)
	defer client.Close()

	if _, err := client.BlockHeight(context.Background()); err == nil {
		t.Fatal("expected error when all endpoints are down, got nil")
	}
}
