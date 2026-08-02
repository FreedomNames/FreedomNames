package content

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Hosting budget: reservations must be atomic and never destructive ---

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
	ix.AddHosted("victim", held, nil, "peer-a", nil)

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
	if !ix.CommitHosted("delivered", offer, nil, "peer-b", budget, budget, 0, future, nil) {
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
