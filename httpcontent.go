package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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

// postContent stores the request body (chunked past chunkSize) and returns its
// content hash. The body is consumed as a stream, so upload size is bounded by
// maxContentSize, not by memory.
func postContent(w http.ResponseWriter, r *http.Request, content *ContentService) {
	// Cap the read one byte past the limit so an oversized upload is detected
	// (PutStream errors when the total crosses maxContentSize) without
	// reading an unbounded body.
	hash, _, err := content.PutStream(r.Context(), io.LimitReader(r.Body, maxContentSize+1))
	if errors.Is(err, ErrContentTooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "content exceeds max size of %d bytes", maxContentSize)
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "store content: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"hash": hash})
}

// getContent serves the bytes for ?hash=, fetching from providers on a miss.
// Chunked content is streamed chunk by chunk rather than assembled in memory.
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
	rc, size, err := content.FetchStream(r.Context(), hash)
	if err != nil {
		writeContentFetchError(w, hash, err)
		return
	}
	defer rc.Close()
	writeContentStream(w, hash, rc, size)
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
		rc, size, err := content.FetchStream(r.Context(), hash)
		if err != nil {
			writeContentFetchError(w, hash, err)
			return
		}
		defer rc.Close()
		w.Header().Set("X-Freedom-Content-Hash", hash)
		writeContentStream(w, hash, rc, size)
	}
}

// writeContentStream streams content bytes with an exact Content-Length. A
// chunk fetch failing mid-stream can only truncate the response (headers are
// already sent); the length mismatch lets the client detect it.
func writeContentStream(w http.ResponseWriter, hash string, rc io.Reader, size int64) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		log.Printf("content: stream %s: %v", hash, err)
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
