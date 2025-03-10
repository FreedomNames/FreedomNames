package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
)

type Response struct {
	Mode            string   `json:"mode"`
	PeerID          string   `json:"peerID"`
	ListenAddresses []string `json:"listenAddresses"`
	Peers           []string `json:"peers"`
	HostsConnected  int      `json:"hostsConnected"`
	NetworkSize     int32    `json:"networkSize"`
	Protocols       []string `json:"protocols"`
}

func StartHTTPServer(freedomDht FreedomDHT, cache Cache) {
	// Set up HTTP API endpoints
	http.HandleFunc("/add", AddHandler(freedomDht, cache))
	http.HandleFunc("/lookup", LookupHandler(freedomDht, cache))
	http.HandleFunc("/peers", AllPeersHandler(freedomDht))
	http.HandleFunc("/info", InfoHandler(freedomDht))
	server := &http.Server{Addr: ":8080", Handler: nil}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		// Creating a channel to listen for signals, like SIGINT
		stop := make(chan os.Signal, 1)
		// Subscribing to interruption signals
		signal.Notify(stop, os.Interrupt)
		// Blocks until the signal is received
		<-stop
		err := server.Shutdown(context.Background())
		if err != nil {
			log.Printf("Error during shutdown: %v\n", err)
		}
		// Notifying the main goroutine that we are done
		wg.Done()
	}()

	log.Println("HTTP API server listening on :8080")
	// Blocking until the server is done
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		// Graceful shutdown the HTTP server
		wg.Wait()
		//log.Println("Server was gracefully shut down.")
	} else if err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// AddHandler stores a domain mapping
func AddHandler(freedomDht FreedomDHT, cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

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
				// PoC: For now we just ignore recordType (A)
				cache.Set(key, value)
				// TODO: Combine the record types and store them as a single value in the DHT
				// For now we just store the first record type (A)

				// Store in DHT
				dhtKey := "/fn/" + key
				log.Println("Store key:", dhtKey)
				if err := freedomDht.PutValue(dhtKey, []byte(value)); err != nil {
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
func LookupHandler(freedomDht FreedomDHT, cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "Missing key parameter", http.StatusBadRequest)
			return
		}

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
		valueBytes, err := freedomDht.GetValue(dhtKey)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to retrieve value from DHT: %v", err), http.StatusInternalServerError)
			return
		}
		value := string(valueBytes)

		w.Header().Set("Content-Type", "application/json")

		// Key not found, return a 404 JSON response
		if value == "" {
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
func AllPeersHandler(freedomDht FreedomDHT) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		// Get all peers from the routing table
		peers := freedomDht.GetRoutingPeers()
		peerList := make([]string, len(peers))

		for i, p := range peers {
			peerList[i] = p.String()
		}

		// Get list of connected hosts
		connectedHosts := freedomDht.GetNetworkPeers()
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
}

// InfoHandler returns general information about the DHT
func InfoHandler(freedomDht FreedomDHT) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		// DHT
		mode := freedomDht.GetMode()
		peerID := freedomDht.GetPeerID()
		hostListenAddrs := freedomDht.GetListenAddresses()
		listenAddrList := make([]string, len(hostListenAddrs))
		for i, listenAddr := range hostListenAddrs {
			listenAddrList[i] = listenAddr.String()
		}

		// Get connected hosts
		hosts := freedomDht.GetNetworkPeers()
		hostsConnected := len(hosts)

		// Network size estimation
		networkSize, err := freedomDht.GetNetworkSize()
		if err != nil {
			networkSize = 0
		}

		// Peer info
		peerInfos := freedomDht.GetPeerInfos()
		infoList := make([]string, len(peerInfos))
		for i, p := range peerInfos {
			infoList[i] = p.Id.String()
		}

		// Get protocols
		protocols := freedomDht.GetProtocols()
		protocolList := make([]string, len(protocols))
		for i, protocol := range protocols {
			// protocol.ID type is just a string
			protocolList[i] = string(protocol)
		}

		response := Response{
			Mode:            mode,
			PeerID:          peerID,
			ListenAddresses: listenAddrList,
			Peers:           infoList,
			HostsConnected:  hostsConnected,
			NetworkSize:     networkSize,
			Protocols:       protocolList,
		}
		jsonResponse, err := json.Marshal(response)
		if err != nil {
			http.Error(w, "Failed to encode peer list", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonResponse)
	}
}
