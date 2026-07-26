package httpapi

import (
	"context"

	kbucket "github.com/libp2p/go-libp2p-kbucket"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// FreedomDHT is the slice of the node this API actually serves: record
// publish/resolve plus the peer and routing stats behind /info and /all-peers.
//
// It is declared here, on the consuming side, rather than in internal/node —
// the same pattern resolver.RecordStore and registry.NameRegistry already
// follow. *node.FreedomNameNode satisfies it structurally, so neither package
// has to import the other.
type FreedomDHT interface {
	IsInitialized() bool
	Shutdown()
	PutValue(key string, value []byte) error
	GetValue(key string) ([]byte, error)
	PublishRecord(rec *record.FNRecord) error
	ResolveRecord(ctx context.Context, key string) (*record.FNRecord, error)
	GetMode() string
	GetPeerInfos() []kbucket.PeerInfo
	GetRoutingPeers() []peer.ID
	GetNetworkPeers() []peer.ID
	GetPeerID() string
	GetListenAddresses() []multiaddr.Multiaddr
	GetNetworkSize() (int32, error)
	GetProtocols() []protocol.ID
}
