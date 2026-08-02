package node

import "testing"

// A peer listed under several transports is one relay candidate and one DHT
// bootstrap peer, not several. AutoRelay and the DHT both take peers rather
// than addresses, so the transports have to be folded into a single AddrInfo.
func TestBootstrapPeerInfosMergesTransportsPerPeer(t *testing.T) {
	const (
		alice = "12D3KooWFRgUQUMvP4rimeZ1vS2DzmP48vvxcfEk5XqWmURMKU13"
		bob   = "12D3KooWJTZUqCzYBT7jrG8ZaxwRU99WSZkKPxTSREYy7EDRsFex"
	)
	infos := BootstrapPeerInfos([]string{
		"/ip4/10.0.0.1/tcp/4020/p2p/" + alice,
		"/ip4/10.0.0.1/udp/4020/quic-v1/p2p/" + alice,
		"/ip4/10.0.0.2/tcp/4020/p2p/" + bob,
		"/ip4/10.0.0.2/udp/4020/quic-v1/p2p/" + bob,
	})

	if len(infos) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(infos))
	}
	if got := countAddrs(infos); got != 4 {
		t.Errorf("expected 4 addresses across the peers, got %d", got)
	}
	for _, info := range infos {
		if len(info.Addrs) != 2 {
			t.Errorf("peer %s kept %d addresses, want both transports", info.ID, len(info.Addrs))
		}
	}
}

// A malformed entry costs only itself: the rest of the list still gets the node
// onto the network, which is why an unreachable or mistyped peer is not fatal.
func TestBootstrapPeerInfosSkipsUnusableEntries(t *testing.T) {
	infos := BootstrapPeerInfos([]string{
		"not-a-multiaddr",
		"/ip4/10.0.0.1/tcp/4020", // valid multiaddr, but names no peer
		"/ip4/10.0.0.2/tcp/4020/p2p/12D3KooWJTZUqCzYBT7jrG8ZaxwRU99WSZkKPxTSREYy7EDRsFex",
	})
	if len(infos) != 1 {
		t.Fatalf("expected the one usable entry, got %d", len(infos))
	}
}

// An empty list is not an error: a node with no bootstrap peers falls back to
// mDNS, and the relay options must simply not be wired to anything.
func TestBootstrapPeerInfosEmpty(t *testing.T) {
	if infos := BootstrapPeerInfos(nil); len(infos) != 0 {
		t.Fatalf("expected no peers, got %d", len(infos))
	}
}
