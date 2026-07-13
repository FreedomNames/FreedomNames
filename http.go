package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"github.com/libp2p/go-libp2p/core/routing"
)

type Response struct {
	Version         string   `json:"version"`
	Mode            string   `json:"mode"`
	PeerID          string   `json:"peerID"`
	ListenAddresses []string `json:"listenAddresses"`
	Peers           []string `json:"peers"`
	HostsConnected  int      `json:"hostsConnected"`
	NetworkSize     int32    `json:"networkSize"`
	Protocols       []string `json:"protocols"`
}

func StartHTTPServer(freedomDht FreedomDHT, resolver *Resolver, cache Cache, content *ContentService, addr string) {
	// Set up HTTP API endpoints
	mux := http.NewServeMux()
	mux.HandleFunc("/publish", PublishHandler(freedomDht))
	mux.HandleFunc("/resolve", ResolveHandler(freedomDht, resolver))
	mux.HandleFunc("/record", RecordHandler(freedomDht))
	mux.HandleFunc("/peers", AllPeersHandler(freedomDht))
	mux.HandleFunc("/info", InfoHandler(freedomDht))
	mux.HandleFunc("/clear_cache", ClearCacheHandler(cache))
	mux.HandleFunc("/health", HealthHandler(freedomDht))
	// Content endpoints (LibreWeb's page-bytes layer).
	mux.HandleFunc("/content", ContentHandler(content))
	mux.HandleFunc("/resolve-content", ResolveContentHandler(resolver, content))
	server := &http.Server{Addr: addr, Handler: mux}

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

	log.Printf("HTTP API server listening on %s", addr)
	// Blocking until the server is done
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		// Graceful shutdown the HTTP server
		wg.Wait()
		//log.Println("Server was gracefully shut down.")
	} else if err != nil {
		if isPrivilegedPortErr(err) || isAddrInUseErr(err) {
			log.Fatalf("HTTP API could not bind %s: %v\n  Set FREEDOM_HTTP_ADDR to a free port, e.g. FREEDOM_HTTP_ADDR=:8421", addr, err)
		}
		log.Fatalf("HTTP server error: %v", err)
	}
}

// PublishHandler stores a pre-signed FNRecord in the DHT. The client is expected
// to have signed the record with the owner's private key (e.g. via the CLI).
func PublishHandler(freedomDht FreedomDHT) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		rec, err := UnmarshalFNRecord(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid FNRecord: %v", err), http.StatusBadRequest)
			return
		}
		// Verify before publishing so we never store an unowned/forged record.
		if err := rec.Verify(); err != nil {
			http.Error(w, fmt.Sprintf("Record failed verification: %v", err), http.StatusBadRequest)
			return
		}
		if err := freedomDht.PublishRecord(rec); err != nil {
			http.Error(w, fmt.Sprintf("Failed to publish record: %v", err), http.StatusInternalServerError)
			return
		}

		name, _ := rec.FullName()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		jsonResponse, _ := json.Marshal(map[string]string{"published": name})
		w.Write(jsonResponse)
	}
}

// ResolveHandler resolves a "label.<pubKeyID>.fn" name to its resource records,
// optionally filtered by ?type=A.
func ResolveHandler(freedomDht FreedomDHT, resolver *Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "Missing name parameter", http.StatusBadRequest)
			return
		}
		recordType := r.URL.Query().Get("type")

		log.Println("Resolve name:", name)

		var (
			records []RR
			err     error
		)
		if recordType != "" {
			records, err = resolver.ResolveType(r.Context(), name, recordType)
		} else {
			records, err = resolver.Resolve(r.Context(), name)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to resolve name: %v", err), resolveErrStatus(err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		jsonResponse, _ := json.Marshal(map[string]any{"name": name, "records": records})
		w.Write(jsonResponse)
	}
}

// resolveErrStatus maps a resolution error to an HTTP status so clients can
// tell "this name does not exist" (404) apart from "bad request" (400) and
// "the lookup infrastructure failed, retry later" (502).
func resolveErrStatus(err error) int {
	switch {
	case errors.Is(err, routing.ErrNotFound), errors.Is(err, ErrRegistryNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrNotFNName):
		return http.StatusBadRequest
	default:
		// Transient/unknown failure (DHT timeout, no peers).
		return http.StatusBadGateway
	}
}

// RecordHandler returns the raw signed FNRecord for a name (including Seq and
// EOL), bypassing the record cache. The CLI uses it to derive the next sequence
// number before publishing an update.
func RecordHandler(freedomDht FreedomDHT) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !freedomDht.IsInitialized() {
			http.Error(w, "DHT not initialized", http.StatusInternalServerError)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "Missing name parameter", http.StatusBadRequest)
			return
		}
		key, err := DHTKeyForName(name)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid name: %v", err), http.StatusBadRequest)
			return
		}
		rec, err := freedomDht.ResolveRecord(r.Context(), key)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch record: %v", err), resolveErrStatus(err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		jsonResponse, _ := json.Marshal(rec)
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
			Version:         nodeVersion,
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

// ClearCacheHandler clears the local cache using a DELETE request
func ClearCacheHandler(cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if the request method is DELETE
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Clear the full cache
		cache.Clear()
	}
}
