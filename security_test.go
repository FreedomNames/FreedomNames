package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
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

// --- log integrity ---

// TestDNSLogsEscapeHostileQueryNames covers log forgery from an unauthenticated
// packet. A query name is raw bytes off the wire — the DNS library escapes
// embedded dots and nothing else — and the .fn zone answers whoever asks, by
// design. Logging one with %s would let anyone write whatever they liked into
// the operator's log, including terminal escapes.
func TestDNSLogsEscapeHostileQueryNames(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	hostile := "evil\n2026/01/01 00:00:00 DNS server listening on 0.0.0.0:53\x1b[2Kgotcha.fn."
	s := &DNSServer{inflight: make(chan struct{}, 1)}
	s.logOverloaded(hostile)

	out := buf.String()
	if n := strings.Count(out, "\n"); n != 1 {
		t.Fatalf("log emitted %d newlines, want 1 (a forged line was injected):\n%s", n, out)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Fatalf("log carried a raw terminal escape:\n%q", out)
	}
	if !strings.Contains(out, "evil") {
		t.Fatalf("log dropped the name entirely, which defeats the point:\n%s", out)
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

// --- registry history scanning ---

// TestNewestHistoryScansTheRecentEnd pins the direction of the custody scan. A
// transaction that spends an output is mined at or after the one that created
// it, so a truncated custody scan has to keep the recent entries: keeping the
// oldest would search precisely the half that cannot contain the spend, report
// the outpoint as unspent, and freeze a transferred name on its previous owner.
func TestNewestHistoryScansTheRecentEnd(t *testing.T) {
	history := make([]electrumHistoryItem, maxHistoryScan+10)
	for i := range history {
		history[i] = electrumHistoryItem{Height: int64(i + 1), TxHash: fmt.Sprintf("tx%d", i)}
	}

	scan, truncated := newestHistory(history, "test")
	if !truncated {
		t.Fatal("truncated = false for an over-long history")
	}
	if len(scan) != maxHistoryScan {
		t.Fatalf("scanned %d entries, want %d", len(scan), maxHistoryScan)
	}
	// Newest first, and the newest entry must be present at all.
	if got, want := scan[0].Height, history[len(history)-1].Height; got != want {
		t.Fatalf("first scanned height = %d, want the newest (%d)", got, want)
	}
	if got, want := scan[len(scan)-1].Height, history[10].Height; got != want {
		t.Fatalf("last scanned height = %d, want %d", got, want)
	}

	short := history[:5]
	if scan, truncated := newestHistory(short, "test"); truncated || len(scan) != 5 {
		t.Fatalf("short history: truncated=%v len=%d, want false/5", truncated, len(scan))
	}

	// The claim scan keeps the other end: the earliest confirmed claim wins.
	scan, truncated = oldestHistory(history, "test")
	if !truncated || len(scan) != maxHistoryScan {
		t.Fatalf("oldestHistory: truncated=%v len=%d", truncated, len(scan))
	}
	if got, want := scan[0].Height, history[0].Height; got != want {
		t.Fatalf("oldestHistory first height = %d, want %d", got, want)
	}
}

// TestTruncatedHistoryIsNotReportedAsUnclaimed covers the denial of service a
// silent truncation would open. The marker address is derived from the label
// alone, so anyone can compute it and pad a name's history with dust until the
// real claim falls outside the scan window. The lookup must then fail as
// inconclusive: reporting "unclaimed" would be wrong AND would be negative
// cached, keeping a perfectly valid name dark long after the flood.
func TestTruncatedHistoryIsNotReportedAsUnclaimed(t *testing.T) {
	m := newMockElectrum(t)
	ownerKey := newTestKey(t)
	ownerPub := ownerPubBytes(t, ownerKey)

	fundingKey, _ := secp256k1.GeneratePrivateKey()
	holderScript := p2pkhScript(hash160(fundingKey.PubKey().SerializeCompressed()))

	// Dust padding: cheap for an attacker, and it all lands on the marker
	// address ahead of the genuine claim.
	dust := makeClaimTx(t, "unrelated", ownerPub, holderScript, fundingKey, mustHex(t, repeat("cd", 32)))
	dustID := m.addTx(dust, 1, markerScript("unrelated"))
	marker := scriptHash(markerScript("padded"))
	for i := range maxHistoryScan + 1 {
		m.history[marker] = append(m.history[marker], electrumHistoryItem{Height: int64(i + 1), TxHash: dustID})
	}

	claim := makeClaimTx(t, "padded", ownerPub, holderScript, fundingKey, mustHex(t, repeat("ab", 32)))
	m.addTx(claim, 10000, markerScript("padded"), holderScript)

	client := newElectrumClient(m.endpoint())
	defer client.Close()
	reg := NewBCHRegistry(client, 1)

	_, err := reg.ResolveOwner("padded.fn")
	if err == nil {
		t.Fatal("expected the truncated lookup to fail")
	}
	if !errors.Is(err, errHistoryTruncated) {
		t.Fatalf("err = %v, want it to wrap errHistoryTruncated", err)
	}
	// The important half: ErrRegistryNotFound is the only error that gets
	// negative cached, so an inconclusive scan must not masquerade as one.
	if errors.Is(err, ErrRegistryNotFound) {
		t.Fatal("an incomplete scan was reported as a definitive not-found, which gets negative cached")
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

// TestAbandonedOfferDestroysNothing covers a remote content-wipe. A push offer
// is a few dozen bytes any peer can send, and nothing obliges that peer to
// follow it with a transfer. If merely making room for the offer evicted, a
// stranger could delete a node's replicas for free — offer, let the stream die,
// repeat — which is precisely the availability the content layer exists to
// provide. Eviction has to wait until the bytes are real.
func TestAbandonedOfferDestroysNothing(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ix, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	const (
		budget = 1000
		held   = 800 // a replica this node already stores for the network
		offer  = 600 // only fits if the held set is evicted
	)
	ix.AddHosted("victim", held, nil, "peer-a")

	// Look at the index from far enough in the future that the held set is past
	// evictionProtection and so genuinely is an eviction candidate.
	future := time.Now().Add(2 * evictionProtection)

	if !ix.Reserve(offer, budget, budget, 0, future) {
		t.Fatal("the offer fits once the held set is evicted, so it should be admitted")
	}
	if !ix.Has("victim") {
		t.Fatal("an offer that never delivered a byte deleted content this node already held")
	}
	if got := ix.HostedBytes(); got != held+offer {
		t.Fatalf("hosted bytes = %d, want %d (held + the outstanding promise)", got, held+offer)
	}

	// The transfer dies. Everything must be exactly as it was.
	ix.Release(offer)
	if !ix.Has("victim") || ix.HostedBytes() != held {
		t.Fatalf("after an abandoned offer: victim held = %v, hosted = %d, want true/%d",
			ix.Has("victim"), ix.HostedBytes(), held)
	}

	// A transfer that actually completes is what earns the right to evict.
	if !ix.Reserve(offer, budget, budget, 0, future) {
		t.Fatal("second offer should be admitted")
	}
	if !ix.CommitHosted("delivered", offer, nil, "peer-b", budget, budget, 0, future) {
		t.Fatal("a fully received set should commit")
	}
	if ix.Has("victim") {
		t.Fatal("commit should have evicted the least-recently-used set to make room")
	}
	if !ix.Has("delivered") {
		t.Fatal("the received set was not recorded")
	}
	// The reservation must be consumed by the commit, not counted a second time.
	if got := ix.HostedBytes(); got != offer {
		t.Fatalf("hosted bytes = %d after commit, want %d", got, offer)
	}
}

// TestReserveIsAtomicUnderConcurrency is the test the first cut of the fix
// would have failed: testing the budget and taking the reservation as two
// separate lock holds leaves both callers passing the same check. Pushes arrive
// on independent streams, so this is the real shape of the race.
func TestReserveIsAtomicUnderConcurrency(t *testing.T) {
	dir := t.TempDir()
	store, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ix, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	const (
		budget  = 10_000
		size    = 1_000
		callers = 64
	)
	// Exactly 10 of the 64 racing reservations can fit in the budget.
	want := budget / size

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		granted atomic.Int64
	)
	now := time.Now()
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them all at once to maximise overlap
			if ix.Reserve(size, budget, budget, time.Hour, now) {
				granted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := granted.Load(); got != int64(want) {
		t.Fatalf("%d concurrent reservations granted, want exactly %d (budget %d / size %d)", got, want, budget, size)
	}
	if got := ix.HostedBytes(); got != int64(want)*size {
		t.Fatalf("reserved bytes = %d, want %d", got, int64(want)*size)
	}
}
