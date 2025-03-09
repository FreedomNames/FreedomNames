package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"os"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	kbucket "github.com/libp2p/go-libp2p-kbucket"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/multiformats/go-multiaddr"
)

type FreedomDHT interface {
	IsInitialized() bool
	Shutdown()
	PutValue(key string, value []byte) error
	GetValue(key string) ([]byte, error)
	GetMode() string
	GetPeerInfos() []kbucket.PeerInfo
	GetRoutingPeers() []peer.ID
	GetNetworkPeers() []peer.ID
	GetPeerID() string
	GetListenAddresses() []multiaddr.Multiaddr
	GetNetworkSize() (int32, error)
}

type FreedomName struct {
	kadDHT *dht.IpfsDHT
	// dualkadDHT *dual.DHT
}

// mDNSNotifee implements the mdns.Notifee interface.
type mDNSNotifee struct {
	host host.Host
}

// HandlePeerFound is called when a new peer is found via mDNS.
func (n *mDNSNotifee) HandlePeerFound(pi peer.AddrInfo) {
	//log.Printf("mDNS discovered peer: %s", pi.ID.String())
	// Attempt to connect to the discovered peer.
	if err := n.host.Connect(context.Background(), pi); err != nil {
		log.Printf("Error connecting to peer %s: %v", pi.ID.String(), err)
	}
}

func NewDHT() *FreedomName {
	freedomName := new(FreedomName)
	serviceName := "FreedomNames/1.0.0"

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error

	// Generate a new private key or load it from a file
	privKey, err := loadOrGenerateKey()
	if err != nil {
		panic(err)
	}

	// In case we want to setup a dual DHT!?
	// routing := libp2p.Routing(func(host host.Host) (routing.PeerRouting, error) {
	// 	dhtOpts := dual.DHTOption(
	// 		dht.Mode(dht.ModeServer),
	// 		dht.Concurrency(10),
	// 		dht.ProtocolPrefix("/freedomnames"),
	// 	)

	// 	var err error
	// 	dualkadDHT, err = dual.New(ctx, host, dhtOpts)
	// 	return dualkadDHT, err
	// })

	// Common options
	opts := []libp2p.Option{
		// routing,
		libp2p.NATPortMap(),
		libp2p.UserAgent(serviceName),
		libp2p.Identity(privKey),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Ping(false),
	}

	// In case of the bootstrap node, we need to listen on a specific port
	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		log.Println("Starting bootstrap node!")
		opts = append(opts, []libp2p.Option{
			libp2p.ListenAddrStrings(
				"/ip4/0.0.0.0/tcp/4020",
				"/ip4/0.0.0.0/udp/4020/quic-v1",
				"/ip4/0.0.0.0/udp/4021/quic-v1/webtransport",
				"/ip4/0.0.0.0/udp/4022/webrtc-direct",
			),
		}...)
	}

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		panic(err)
	}

	log.Printf("Peer ID: %s", p2pHost.ID().String())
	log.Printf("Connect to me on:")
	hostAddrs := p2pHost.Addrs()
	for _, addr := range hostAddrs {
		log.Printf("  %s/p2p/%s", addr, p2pHost.ID().String())
	}

	// Set up mDNS discovery to find peers on the local network.
	mdnsService := mdns.NewMdnsService(p2pHost, "localfreedomnames", &mDNSNotifee{host: p2pHost})
	if err := mdnsService.Start(); err != nil {
		panic(err)
	} else {
		log.Println("mDNS service started")
	}

	// Define a list of bootstrap peers.
	bootstrapPeers := []string{
		"/ip4/192.168.1.204/tcp/4020/p2p/12D3KooWKsFK44rGGDuemE9cw8mkcHLM1k7x3uNDjAz3Ts29D8GZ",
		// "/dnsaddr/domain.name/p2p/aaaa"
	}
	bootstrapInfos := BootstrapPeerInfos(bootstrapPeers)

	// DHT options
	dhtOpts := []dht.Option{
		dht.BucketSize(10),
		dht.ProtocolPrefix("/freedomnames"),
		dht.Concurrency(15),
		dht.EnableOptimisticProvide(), // Enable experimental optimistic provide, which will store the provider record that has a even closer peer.
		dht.Resiliency(2),
		dht.Validator(record.NamespacedValidator{
			"fn": FreedomNameValidator{},
		}),
	}

	// If in bootstrap mode become server and do not bootstrap
	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		// Start the DHT in server mode
		dhtOpts = append(dhtOpts, dht.Mode(dht.ModeServer))
	} else {
		// Start the DHT in client mode we will use bootstrap peers.
		// And use the default Auto DHT mode.
		dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootstrapInfos...))
	}

	// Create a new Kademlia DHT instance using the host
	freedomName.kadDHT, err = dht.New(ctx, p2pHost, dhtOpts...)
	if err != nil {
		panic(err)
	}

	// Bootstrap the DHT node
	if err = freedomName.kadDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}
	return freedomName
}

// Check if DHT & host are initialized, true if both are initialized
func (freedomName *FreedomName) IsInitialized() bool {
	return freedomName.kadDHT != nil && freedomName.kadDHT.Host() != nil
}

// Shutdown shuts down the host and the DHT
func (freedomName *FreedomName) Shutdown() {
	// Close the host
	if host := freedomName.kadDHT.Host(); host != nil {
		host.Close()
	}

	if freedomName.kadDHT != nil {
		// Close the DHT
		if err := freedomName.kadDHT.Close(); err != nil {
			log.Printf("Error closing DHT: %v", err)
		}
	}
}

// Get mode
func (freedomName *FreedomName) GetMode() string {
	if freedomName.kadDHT != nil {
		modeStr := "Unknown"
		switch freedomName.kadDHT.Mode() {
		case dht.ModeAuto:
			modeStr = "Auto"
		case dht.ModeClient:
			modeStr = "Client"
		case dht.ModeServer:
			modeStr = "Server"
		case dht.ModeAutoServer:
			modeStr = "AutoServer"
		default:
			modeStr = "Unknown"
		}
		return modeStr
	}
	return "Not initialized"
}

// Get routing peer infos
func (freedomName *FreedomName) GetPeerInfos() []kbucket.PeerInfo {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.RoutingTable().GetPeerInfos()
	}
	return nil
}

// Get all routing peers
func (freedomName *FreedomName) GetRoutingPeers() []peer.ID {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.RoutingTable().ListPeers()
	}
	return nil
}

// Get all network peers
func (freedomName *FreedomName) GetNetworkPeers() []peer.ID {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.Host().Network().Peers()
	}
	return nil
}

// PutValue add value to DHT
func (freedomName *FreedomName) PutValue(key string, value []byte) error {
	if freedomName.kadDHT != nil {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		return freedomName.kadDHT.PutValue(ctx, key, value)
	}
	return errors.New("DHT not initialized")
}

// GetValue get value from DHT
func (freedomName *FreedomName) GetValue(key string) ([]byte, error) {
	if freedomName.kadDHT != nil {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		return freedomName.kadDHT.GetValue(ctx, key)
	}
	return nil, errors.New("DHT not initialized")
}

// Get peer ID
func (freedomName *FreedomName) GetPeerID() string {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.PeerID().String()
	}
	return ""
}

// Get all listen addresses
func (freedomName *FreedomName) GetListenAddresses() []multiaddr.Multiaddr {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.Host().Addrs()
	}
	return nil
}

// Get approximate size of the DHT
func (freedomName *FreedomName) GetNetworkSize() (int32, error) {
	if freedomName.kadDHT != nil {
		return freedomName.kadDHT.NetworkSize()
	}
	return 0, errors.New("DHT not initialized")
}

// -----------------------------------------------------------
// Private functions
// -----------------------------------------------------------

func BootstrapPeerInfos(addrs []string) []peer.AddrInfo {
	var infos []peer.AddrInfo
	for _, s := range addrs {
		maddr, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			log.Printf("error parsing multiaddr %s: %v", s, err)
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			log.Printf("error converting multiaddr %s to AddrInfo: %v", s, err)
			continue
		}
		infos = append(infos, *info)
	}
	return infos
}

func loadOrGenerateKey() (crypto.PrivKey, error) {
	keyFile := "private.key"
	// Check if key file exists
	if _, err := os.Stat(keyFile); err == nil {
		// Load key from file
		keyData, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, err
		}
		return crypto.UnmarshalPrivateKey(keyData)
	}

	// Generate a new private key
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
	if err != nil {
		return nil, err
	}

	// Save the key to file
	keyData, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	os.WriteFile(keyFile, keyData, 0600) // Store securely

	return priv, nil
}
