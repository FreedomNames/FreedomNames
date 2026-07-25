package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- HTTP API guard ---

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
		wantStatus int
	}{
		{"plain GET", http.MethodGet, "localhost:8420", "", http.StatusOK},
		{"plain POST (curl/CLI, no Origin)", http.MethodPost, "localhost:8420", "", http.StatusOK},
		{"same-origin POST", http.MethodPost, "localhost:8420", "http://localhost:8420", http.StatusOK},
		{"rebound host", http.MethodGet, "attacker.example:8420", "", http.StatusForbidden},
		{"cross-origin POST", http.MethodPost, "localhost:8420", "https://attacker.example", http.StatusForbidden},
		{"cross-origin DELETE", http.MethodDelete, "localhost:8420", "https://attacker.example", http.StatusForbidden},
		// A cross-origin GET leaks nothing (the reply is unreadable without
		// CORS headers, which we never send) so it is not blocked.
		{"cross-origin GET", http.MethodGet, "localhost:8420", "https://attacker.example", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(c.method, "http://"+c.host+"/publish", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
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

func TestForwardingAllowedOnlyForLocalClients(t *testing.T) {
	cases := []struct {
		addr net.Addr
		want bool
	}{
		{&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("::1"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("192.168.1.20"), Port: 5000}, true},
		{&net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 5000}, true},
		{&net.UDPAddr{IP: net.ParseIP("169.254.1.1"), Port: 5000}, true},
		// The open-resolver case: a stranger on the internet must not be able
		// to bounce arbitrary queries off this node.
		{&net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 5000}, false},
		{&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5000}, false},
		{&net.UDPAddr{IP: net.ParseIP("2606:4700::1111"), Port: 5000}, false},
	}
	for _, c := range cases {
		if got := forwardingAllowed(c.addr); got != c.want {
			t.Errorf("forwardingAllowed(%v) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// --- record size limits ---

func TestValidateRecordsEnforcesSizeLimits(t *testing.T) {
	tests := []struct {
		name    string
		rec     FNRecord
		wantErr string
	}{
		{
			name:    "TXT longer than a DNS character-string",
			rec:     FNRecord{Label: "x", Records: []RR{{Type: RecordTypeTXT, Value: strings.Repeat("a", maxTXTLen+1)}}},
			wantErr: "TXT value",
		},
		{
			name:    "too many resource records",
			rec:     FNRecord{Label: "x", Records: manyRecords(maxRRsPerRecord + 1)},
			wantErr: "resource records",
		},
		{
			name:    "oversized label",
			rec:     FNRecord{Label: strings.Repeat("a", maxLabelLen+1), Records: []RR{{Type: RecordTypeA, Value: "10.0.0.1"}}},
			wantErr: "label is",
		},
		{
			name:    "oversized CNAME target",
			rec:     FNRecord{Label: "x", Records: []RR{{Type: RecordTypeCNAME, Value: strings.Repeat("a", maxDNSNameLen+1)}}},
			wantErr: "CNAME target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rec.validateRecords()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}

	// The limits must not reject an ordinary record set.
	ok := FNRecord{Label: "mysite", Records: []RR{
		{Type: RecordTypeA, Value: "10.0.0.1", TTL: 300},
		{Type: RecordTypeTXT, Value: strings.Repeat("a", maxTXTLen), TTL: 300},
	}}
	if err := ok.validateRecords(); err != nil {
		t.Fatalf("valid record set rejected: %v", err)
	}
}

func manyRecords(n int) []RR {
	out := make([]RR, n)
	for i := range out {
		out[i] = RR{Type: RecordTypeTXT, Value: "v", TTL: 300}
	}
	return out
}

// --- CLI label validation ---

func TestCheckLabelRejectsPathTraversal(t *testing.T) {
	bad := []string{
		"", "..", ".", "../../etc/passwd", "a/b", `a\b`, "a..b", "-lead", "sp ace", "nul\x00l",
	}
	for _, label := range bad {
		if err := checkLabel(label); err == nil {
			t.Errorf("checkLabel(%q) = nil, want an error", label)
		}
	}
	good := []string{"mysite", "blog.mysite", "my-site", "my_site", "site123"}
	for _, label := range good {
		if err := checkLabel(label); err != nil {
			t.Errorf("checkLabel(%q) = %v, want nil", label, err)
		}
	}
	// keyPath must refuse to build a path outside the keys directory.
	if _, err := keyPath("../../escape"); err == nil {
		t.Error("keyPath escaped the keys directory")
	}
}

// --- hosting budget ---

// TestReserveBlocksConcurrentBudgetOvershoot proves the fix for the push path:
// admitting a set before its bytes arrive must count the promise, or N
// simultaneous pushes each see an empty store and all get accepted.
func TestReserveBlocksConcurrentBudgetOvershoot(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ix, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	const budget = 1000
	const size = 600
	now := time.Now()

	if !ix.Reserve(size, budget, budget, time.Hour, now) {
		t.Fatal("first reservation should be admitted")
	}
	if ix.Reserve(size, budget, budget, time.Hour, now) {
		t.Fatal("second concurrent reservation exceeded the budget and should have been refused")
	}
	// Once the first transfer finishes (or fails) the room comes back.
	ix.Release(size)
	if !ix.Reserve(size, budget, budget, time.Hour, now) {
		t.Fatal("reservation should be admitted after the earlier one was released")
	}
	ix.Release(size)
	if got := ix.HostedBytes(); got != 0 {
		t.Fatalf("hosted bytes = %d after releasing every reservation, want 0", got)
	}
	// Releases must not drive the counter negative and hand out free budget.
	ix.Release(size)
	if got := ix.HostedBytes(); got != 0 {
		t.Fatalf("hosted bytes = %d after an extra release, want 0", got)
	}
}
