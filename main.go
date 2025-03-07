package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	ma "github.com/multiformats/go-multiaddr"
)

// Global variables
var (
	serviceName = "FreedomNames/1.0.0"
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

	// priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	// if err != nil {
	// 	return nil, err
	// }

	// Create a new libp2p host with default options
	p2pHost, err = libp2p.New(
		libp2p.NATPortMap(),
		libp2p.UserAgent(serviceName),
		// TODO: Create a local private key once and use it: libp2p.Identity(priveKey),
		// Security options:
		libp2p.Security(noise.ID, noise.New),
		libp2p.Ping(false),
	)
	if err != nil {
		log.Fatalf("Failed to create libp2p host: %v", err)
	}
	log.Printf("Libp2p host created. ID: %s", p2pHost.ID().String()) // Fixed .Pretty() issue

	// Create a new Kademlia DHT instance using the host
	kadDHT, err = dht.New(ctx, p2pHost)
	if err != nil {
		log.Fatalf("Failed to create DHT instance: %v", err)
	}

	// Bootstrap the DHT node
	if err = kadDHT.Bootstrap(ctx); err != nil {
		log.Fatalf("Failed to bootstrap DHT: %v", err)
	}

	// Connect to bootstrap peers
	bootstrapPeers := []string{
		"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	}
	for _, addrStr := range bootstrapPeers {
		addr, err := ma.NewMultiaddr(addrStr)
		if err != nil {
			log.Printf("Invalid bootstrap address %q: %v", addrStr, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Printf("Failed to get peer info for %q: %v", addrStr, err)
			continue
		}
		if err := p2pHost.Connect(ctx, *pi); err != nil {
			log.Printf("Error connecting to bootstrap peer %q: %v", pi.ID.String(), err) // Fixed .Pretty() issue
		} else {
			log.Printf("Connected to bootstrap peer: %s", pi.ID.String()) // Fixed .Pretty() issue
		}
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

// addHandler stores a domain mapping
func addHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

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
			dhtKey := "/freedomnames/" + key
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
		w.Write([]byte(value))
		return
	}
	cacheLock.RUnlock()

	// Fetch from DHT
	dhtKey := "/freedomnames/" + key
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
	jsonResponse, _ := json.Marshal(map[string]string{"key": key, "value": value})
	w.Write(jsonResponse)
}

// allPeersHandler retrieves a list of connected peers from the DHT routing table
func allPeersHandler(w http.ResponseWriter, r *http.Request) {
	if kadDHT == nil {
		http.Error(w, "DHT not initialized", http.StatusInternalServerError)
		return
	}

	rtb := kadDHT.RoutingTable()
	if rtb == nil {
		http.Error(w, "Routing table is nil", http.StatusInternalServerError)
		return
	}

	peers := rtb.ListPeers() // Get all peers from the routing table
	peerList := make([]string, len(peers))

	for i, p := range peers {
		peerList[i] = p.String() // Convert peer.ID to string
	}

	jsonResponse, err := json.Marshal(peerList)
	if err != nil {
		http.Error(w, "Failed to encode peer list", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
