package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// This file holds the content-layer HTTP endpoints LibreWeb depends on:
// POST /content (store bytes), GET /content?hash= (fetch bytes), and
// GET /resolve-content?name= (name -> CONTENT record -> bytes in one call).
// Plus /health for the spawned-node handshake.

// writeJSONError writes a typed JSON error so the browser can show a friendly
// message and branch on the status code.
func writeJSONError(w http.ResponseWriter, status int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

// ContentHandler stores a blob (POST) or serves one by hash (GET). It replaces
// `ipfs add` / `ipfs cat` for LibreWeb.
func ContentHandler(content *ContentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if content == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "content service not enabled")
			return
		}
		switch r.Method {
		case http.MethodPost:
			postContent(w, r, content)
		case http.MethodGet:
			getContent(w, r, content)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "use POST to store or GET to fetch")
		}
	}
}

// postContent stores the raw request body and returns its content hash.
func postContent(w http.ResponseWriter, r *http.Request, content *ContentService) {
	// Cap the read so an oversized upload can't exhaust memory.
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBlobSize+1))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: %v", err)
		return
	}
	if len(data) > maxBlobSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "content exceeds max size of %d bytes", maxBlobSize)
		return
	}
	hash, err := content.Put(r.Context(), data)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store content: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"hash": hash})
}

// getContent serves the bytes for ?hash=, fetching from providers on a miss.
func getContent(w http.ResponseWriter, r *http.Request, content *ContentService) {
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		writeJSONError(w, http.StatusBadRequest, "missing hash parameter")
		return
	}
	if !isContentHash(hash) {
		writeJSONError(w, http.StatusBadRequest, "invalid content hash")
		return
	}
	data, err := content.Fetch(r.Context(), hash)
	if err != nil {
		writeContentFetchError(w, hash, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ResolveContentHandler resolves a name to its CONTENT record and streams the
// bytes in a single call — the request LibreWeb makes for every page load.
func ResolveContentHandler(resolver *Resolver, content *ContentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if content == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "content service not enabled")
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "missing name parameter")
			return
		}

		records, err := resolver.ResolveType(r.Context(), name, RecordTypeCONTENT)
		if err != nil {
			writeJSONError(w, resolveErrStatus(err), "resolve %s: %v", name, err)
			return
		}
		if len(records) == 0 {
			writeJSONError(w, http.StatusNotFound, "%s has no CONTENT record", name)
			return
		}

		hash := records[0].Value
		data, err := content.Fetch(r.Context(), hash)
		if err != nil {
			writeContentFetchError(w, hash, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Freedom-Content-Hash", hash)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

// writeContentFetchError maps a Fetch error to a status: 404 if genuinely not
// available anywhere, 502 for transient discovery/transfer failures.
func writeContentFetchError(w http.ResponseWriter, hash string, err error) {
	if errors.Is(err, ErrBlobNotFound) {
		writeJSONError(w, http.StatusNotFound, "content %s not found on the network", hash)
		return
	}
	writeJSONError(w, http.StatusBadGateway, "fetch content %s: %v", hash, err)
}

// HealthHandler is a stable liveness + version endpoint LibreWeb polls to
// confirm the spawned node is up and is the expected build.
func HealthHandler(freedomDht FreedomDHT) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": nodeVersion,
			"ready":   freedomDht.IsInitialized(),
		})
	}
}
