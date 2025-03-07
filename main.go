package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	ma "github.com/multiformats/go-multiaddr"
)

// Global variables
var (
	serviceName = "FreedomNames/1.0.0"
	keyFile     = "private.key"
	cache       = make(map[string]string)
	cacheLock   sync.RWMutex

	p2pHost host.Host
	kadDHT  *dht.IpfsDHT
)

func main() {
	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var err error

	// Generate a new private key or load it from a file
	privKey, err := loadOrGenerateKey()
	if err != nil {
		fmt.Println("Failed to load key:", err)
		return
	}

	// Common options
	opts := []libp2p.Option{
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

	p2pHost, err = libp2p.New(opts...)
	if err != nil {
		log.Fatalf("Failed to create libp2p host: %v", err)
	}
	log.Printf("Peer ID: %s", p2pHost.ID().String())

	// Define a list of bootstrap peers.
	bootstrapPeers := []string{
		"/ip4/192.168.1.204/tcp/4020/p2p/12D3KooWKsFK44rGGDuemE9cw8mkcHLM1k7x3uNDjAz3Ts29D8GZ",
		//"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
		//"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	}
	bootstrapInfos := BootstrapPeerInfos(bootstrapPeers)

	// Create a new Kademlia DHT instance using the host
	kadDHT, err = dht.New(
		ctx,
		p2pHost,
		//dht.Mode(dht.ModeServer),
		dht.BucketSize(30),
		dht.ProtocolPrefix("/freedomnames"),
		dht.Validator(record.NamespacedValidator{
			"fn": FreedomNameValidator{},
		}),
		dht.BootstrapPeers(bootstrapInfos...),
	)
	if err != nil {
		log.Fatalf("Failed to create DHT instance: %v", err)
	}

	// Bootstrap the DHT node
	if err = kadDHT.Bootstrap(ctx); err != nil {
		log.Fatalf("Failed to bootstrap DHT: %v", err)
	}

	// Set up HTTP API endpoints
	http.HandleFunc("/add", addHandler)
	http.HandleFunc("/lookup", lookupHandler)
	http.HandleFunc("/peers", allPeersHandler)

	// Run the HTTP server
	go func() {
		log.Println("HTTP API server listening on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Keep the program running
	select {}
}

func BootstrapPeerInfos(addrs []string) []peer.AddrInfo {
	var infos []peer.AddrInfo
	for _, s := range addrs {
		maddr, err := ma.NewMultiaddr(s)
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

// addHandler stores a domain mapping
func addHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Add TTL
	// Valid request body (JSON):
	// {
	// 	"test.local": {
	// 		"A": "192.168.1.42"
	// 	}
	// }
	var data map[string]map[string]string
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(body, &data); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	for key, records := range data {
		// The domain is the key
		for _, value := range records {
			// Store in local cache
			cacheLock.Lock()
			// PoC: For now we just ignore recordType (A)
			cache[key] = value
			cacheLock.Unlock()
			// TODO: Combine the record types and store them as a single value in the DHT
			// For now we just store the first record type (A)

			// Store in DHT
			dhtKey := "/fn/" + key
			log.Println("Store key:", dhtKey)
			if err := kadDHT.PutValue(ctx, dhtKey, []byte(value)); err != nil {
				http.Error(w, fmt.Sprintf("Failed to store value in DHT: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Key/Value added successfully"))
}

// lookupHandler retrieves the value from the local cache or DHT
func lookupHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Missing key parameter", http.StatusBadRequest)
		return
	}

	log.Println("Check for key:", key)

	// Check local cache
	cacheLock.RLock()
	if value, found := cache[key]; found {
		cacheLock.RUnlock()
		// PoC: Just return the A record
		w.WriteHeader(http.StatusOK)
		jsonResponse, _ := json.Marshal(map[string]string{key: value})
		w.Write(jsonResponse)
		return
	}
	cacheLock.RUnlock()

	// Fetch from DHT
	dhtKey := "/fn/" + key
	valueBytes, err := kadDHT.GetValue(ctx, dhtKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to retrieve value from DHT: %v", err), http.StatusInternalServerError)
		return
	}
	value := string(valueBytes)

	w.Header().Set("Content-Type", "application/json")

	// Key not found, return a 404 JSON response
	if value != "" {
		// http error no response found
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	// Store in local cache
	cacheLock.Lock()
	cache[key] = value
	cacheLock.Unlock()

	// Key found, return JSON response
	w.WriteHeader(http.StatusOK)
	jsonResponse, _ := json.Marshal(map[string]string{key: value})
	w.Write(jsonResponse)
}

// allPeersHandler retrieves a list of connected peers from the DHT routing table
func allPeersHandler(w http.ResponseWriter, r *http.Request) {
	if kadDHT == nil {
		http.Error(w, "DHT not initialized", http.StatusInternalServerError)
		return
	}

	// Get the routing table
	rtb := kadDHT.RoutingTable()
	if rtb == nil {
		http.Error(w, "Routing table is nil", http.StatusInternalServerError)
		return
	}

	// Get all peers from the routing table
	peers := rtb.ListPeers()
	peerList := make([]string, len(peers))

	for i, p := range peers {
		peerList[i] = p.String()
	}

	// Get list of connected hosts
	connectedHosts := p2pHost.Network().Peers()
	hostList := make([]string, len(connectedHosts))
	for i, host := range connectedHosts {
		hostList[i] = host.String()
	}

	jsonResponse, err := json.Marshal(map[string][]string{"peers": peerList, "hosts": hostList})
	if err != nil {
		http.Error(w, "Failed to encode peer list", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
