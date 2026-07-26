package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- HTTP API guard: Host/Origin checks against DNS rebinding and CSRF ---

func TestHostAllowed(t *testing.T) {
	cases := []struct {
		host    string
		allowed []string
		want    bool
	}{
		{"localhost:8420", nil, true},
		{"127.0.0.1:8420", nil, true},
		{"[::1]:8420", nil, true},
		{"192.168.1.10:8420", nil, true},
		{"LOCALHOST:8420", nil, true},
		{"", nil, true},
		// A rebound attacker domain pointed at 127.0.0.1 is the whole point of
		// the check: the connection succeeds, the Host header gives it away.
		{"attacker.example:8420", nil, false},
		{"node.internal:8420", []string{"node.internal"}, true},
		{"attacker.example:8420", []string{"node.internal"}, false},
		// An operator naturally writes the allow-list entry the way they type
		// the URL. Both sides get normalized, so it still matches.
		{"node.internal:8420", []string{"node.internal:8420"}, true},
		{"node.internal:8420", []string{" NODE.INTERNAL "}, true},
	}
	for _, c := range cases {
		if got := hostAllowed(c.host, c.allowed); got != c.want {
			t.Errorf("hostAllowed(%q, %v) = %v, want %v", c.host, c.allowed, got, c.want)
		}
	}
}

func TestLocalAPIGuardRejectsRebindingAndCrossOrigin(t *testing.T) {
	var reached bool
	guarded := localAPIGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}), nil)

	cases := []struct {
		name       string
		method     string
		host       string
		origin     string
		fetchSite  string
		wantStatus int
	}{
		{"plain GET", http.MethodGet, "localhost:8420", "", "", http.StatusOK},
		{"plain POST (curl/CLI, no Origin)", http.MethodPost, "localhost:8420", "", "", http.StatusOK},
		{"same-origin POST", http.MethodPost, "localhost:8420", "http://localhost:8420", "same-origin", http.StatusOK},
		{"rebound host", http.MethodGet, "attacker.example:8420", "", "", http.StatusForbidden},
		{"cross-origin POST", http.MethodPost, "localhost:8420", "https://attacker.example", "", http.StatusForbidden},
		{"cross-origin DELETE", http.MethodDelete, "localhost:8420", "https://attacker.example", "", http.StatusForbidden},
		{"cross-origin GET", http.MethodGet, "localhost:8420", "https://attacker.example", "", http.StatusForbidden},
		// A sandboxed iframe sends Origin: null, so an attacker's page can
		// choose that value: it must not be treated as "no Origin".
		{"null origin POST", http.MethodPost, "localhost:8420", "null", "", http.StatusForbidden},
		// The case an Origin check cannot see at all: a no-cors GET (<img src>,
		// fetch mode:"no-cors") sends no Origin, and GET is not a safe method
		// here — /content fetches from the network and announces this node as a
		// provider of whatever it fetched.
		{"drive-by no-cors GET", http.MethodGet, "localhost:8420", "", "cross-site", http.StatusForbidden},
		{"cross-site POST", http.MethodPost, "localhost:8420", "", "cross-site", http.StatusForbidden},
		// Typed URL / bookmark, and a page on this same site: both legitimate.
		{"user-typed URL", http.MethodGet, "localhost:8420", "", "none", http.StatusOK},
		{"same-site page", http.MethodGet, "localhost:8420", "", "same-site", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(c.method, "http://"+c.host+"/publish", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", c.fetchSite)
			}
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if want := c.wantStatus == http.StatusOK; reached != want {
				t.Fatalf("handler reached = %v, want %v", reached, want)
			}
		})
	}
}

// --- DNS recursion ---
