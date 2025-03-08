package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	dht "github.com/libp2p/go-libp2p-kad-dht"
)

type Response struct {
	Mode            string   `json:"mode"`
	PeerID          string   `json:"peerID"`
	ListenAddresses []string `json:"listenAddresses"`
	Peers           []string `json:"peers"`
}

func StartHTTPServer(cache Cache) {
	// Set up HTTP API endpoints
	http.HandleFunc("/add", AddHandler(cache))
	http.HandleFunc("/lookup", LookupHandler(cache))
	http.HandleFunc("/peers", AllPeersHandler)
	http.HandleFunc("/info", InfoHandler)

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

// AddHandler stores a domain mapping
func AddHandler(cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if kadDHT == nil {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

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
				// PoC: For now we just ignore recordType (A)
				cache.Set(key, value)
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
}

// LookupHandler retrieves the value from the local cache or DHT
func LookupHandler(cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if kadDHT == nil {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		log.Println("Check for key:", key)

		// Fist check local cache
		if value, found := cache.Get(key); found {
			// PoC: Just return the A record
			w.WriteHeader(http.StatusOK)
			jsonResponse, _ := json.Marshal(map[string]string{key: value})
			w.Write(jsonResponse)
			return
		}

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
		cache.Set(key, value)

		// Key found, return JSON response
		w.WriteHeader(http.StatusOK)
		jsonResponse, _ := json.Marshal(map[string]string{key: value})
		w.Write(jsonResponse)
	}
}

// AllPeersHandler retrieves a list of connected peers from the DHT routing table
func AllPeersHandler(w http.ResponseWriter, r *http.Request) {
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

// InfoHandler returns general information about the DHT
func InfoHandler(w http.ResponseWriter, r *http.Request) {
	if kadDHT == nil {
		http.Error(w, "DHT not initialized", http.StatusInternalServerError)
		return
	}

	// DHT
	mode := kadDHT.Mode()
	modeStr := "Unknown"
	switch mode {
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

	peerID := kadDHT.PeerID().String()
	hostListenAddrs := kadDHT.Host().Addrs()
	listenAddrList := make([]string, len(hostListenAddrs))
	for i, listenAddr := range hostListenAddrs {
		listenAddrList[i] = listenAddr.String()
	}

	// Get the routing table
	rtb := kadDHT.RoutingTable()
	if rtb == nil {
		http.Error(w, "Routing table is nil", http.StatusInternalServerError)
		return
	}

	// Peer info
	peerInfos := rtb.GetPeerInfos()
	infoList := make([]string, len(peerInfos))
	for i, p := range peerInfos {
		infoList[i] = p.Id.String()
	}

	response := Response{
		Mode:            modeStr,
		PeerID:          peerID,
		ListenAddresses: listenAddrList,
		Peers:           infoList,
	}
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Failed to encode peer list", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
