package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/authoring"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

// The authoring API can use owner private keys. It is deliberately a local
// management surface even when the operator exposes the node's ordinary HTTP
// API on a LAN or public address.
func localAuthoringOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			writeJSONError(w, http.StatusForbidden, "the authoring API is available only over loopback")
			return
		}
		requestHost := normalizeHost(r.Host)
		requestIP := net.ParseIP(requestHost)
		if requestHost != "localhost" && (requestIP == nil || !requestIP.IsLoopback()) {
			writeJSONError(w, http.StatusForbidden, "the authoring API must be addressed through localhost")
			return
		}
		// Do not let an HTTP reverse proxy turn a local socket connection into a
		// remote signing capability. A correctly configured proxy normally adds
		// one of these headers; authoring clients have no reason to send either.
		if r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" {
			writeJSONError(w, http.StatusForbidden, "the authoring API cannot be used through a proxy")
			return
		}
		// Port 8420 and the authoring port are different origins but still the
		// same site. Reject browser requests from that weaker boundary even when
		// a no-cors GET carries no Origin header at all.
		if site := r.Header.Get("Sec-Fetch-Site"); site == "same-site" || site == "cross-site" {
			writeJSONError(w, http.StatusForbidden, "the authoring API accepts only same-origin browser requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// NamesHandler lists locally owned names or creates a new owner key.
//
//	GET  /authoring/names
//	POST /authoring/names {"label":"blog"}
func NamesHandler(service *authoring.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authoring service not enabled")
			return
		}
		switch r.Method {
		case http.MethodGet:
			names, err := service.ListNames()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "list names: %v", err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"names": names})
		case http.MethodPost:
			var input struct {
				Label string `json:"label"`
			}
			if err := decodeAuthoringJSON(r, &input); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request: %v", err)
				return
			}
			name, err := service.CreateName(input.Label)
			if err != nil {
				writeAuthoringError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, name)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "use GET to list names or POST to create one")
		}
	}
}

// NamePublishHandler builds, signs and publishes one complete resource-record
// set using the locally held owner key.
//
//	POST /authoring/names/<label>/publish {"records":[...]}
func NamePublishHandler(service *authoring.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authoring service not enabled")
			return
		}
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "use POST to publish a name")
			return
		}
		const prefix = "/authoring/names/"
		const suffix = "/publish"
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
			writeJSONError(w, http.StatusNotFound, "unknown authoring endpoint")
			return
		}
		label := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
		if label == "" || strings.Contains(label, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid name label")
			return
		}
		var input struct {
			Records []record.RR `json:"records"`
		}
		if err := decodeAuthoringJSON(r, &input); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request: %v", err)
			return
		}
		rec, err := service.Publish(r.Context(), label, input.Records)
		if err != nil {
			writeAuthoringError(w, err)
			return
		}
		name, err := rec.FullName()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "derive published name: %v", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"published": name,
			"seq":       rec.Seq,
			"expires":   rec.EOL,
		})
	}
}

func decodeAuthoringJSON(r *http.Request, dst any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxPublishBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxPublishBody {
		return fmt.Errorf("request exceeds %d bytes", maxPublishBody)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("request must contain one JSON object")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	return nil
}

func writeAuthoringError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, authoring.ErrInvalidLabel), errors.Is(err, authoring.ErrInvalidRecords):
		writeJSONError(w, http.StatusBadRequest, "%v", err)
	case errors.Is(err, authoring.ErrNameNotFound):
		writeJSONError(w, http.StatusNotFound, "%v", err)
	case errors.Is(err, authoring.ErrNameExists):
		writeJSONError(w, http.StatusConflict, "%v", err)
	case errors.Is(err, authoring.ErrSequenceExhausted):
		writeJSONError(w, http.StatusConflict, "%v", err)
	case errors.Is(err, authoring.ErrPublisherNotReady):
		writeJSONError(w, http.StatusServiceUnavailable, "%v", err)
	case errors.Is(err, authoring.ErrCurrentRecordUnavailable):
		writeJSONError(w, http.StatusBadGateway, "%v", err)
	default:
		writeJSONError(w, http.StatusInternalServerError, "authoring operation failed: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
