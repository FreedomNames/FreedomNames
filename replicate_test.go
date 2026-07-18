package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestPushOfferFraming(t *testing.T) {
	root, _ := contentHash([]byte("some content"))
	var buf bytes.Buffer
	if err := writePushOffer(&buf, root, 12345, 3); err != nil {
		t.Fatalf("write offer: %v", err)
	}
	gotRoot, size, nblobs, err := readPushOffer(&buf)
	if err != nil {
		t.Fatalf("read offer: %v", err)
	}
	if gotRoot != root || size != 12345 || nblobs != 3 {
		t.Fatalf("offer round trip: %s %d %d", gotRoot, size, nblobs)
	}

	// Invalid offers are rejected.
	bad := [][]byte{}
	var b1 bytes.Buffer
	writePushOffer(&b1, "not-a-hash", 10, 1)
	bad = append(bad, b1.Bytes())
	var b2 bytes.Buffer
	writePushOffer(&b2, root, 0, 1) // zero size
	bad = append(bad, b2.Bytes())
	var b3 bytes.Buffer
	writePushOffer(&b3, root, 10, 0) // zero blobs
	bad = append(bad, b3.Bytes())
	var b4 bytes.Buffer
	writePushOffer(&b4, root, maxContentSize+1, 1) // over max
	bad = append(bad, b4.Bytes())
	for i, data := range bad {
		if _, _, _, err := readPushOffer(bytes.NewReader(data)); err == nil {
			t.Errorf("bad offer %d accepted", i)
		}
	}
}

// pushTestPeer is one side of a two-real-hosts push test: a ContentService
// with a real store and index but no DHT node.
type pushTestPeer struct {
	cs   *ContentService
	host host.Host
}

func newPushTestPeer(t *testing.T, budget int64) *pushTestPeer {
	t.Helper()
	dir := t.TempDir()
	store, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ix, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	h := newTestHost(t)
	t.Cleanup(func() { h.Close() })
	cs := &ContentService{
		store:       store,
		index:       ix,
		hostBudget:  budget,
		maxPushSize: maxContentSize,
		hostTTL:     24 * time.Hour,
	}
	h.SetStreamHandler(pushProtocol, cs.handlePushStream)
	return &pushTestPeer{cs: cs, host: h}
}

// pushBetween connects sender -> receiver and runs a push of root over a real
// libp2p stream.
func pushBetween(t *testing.T, sender, receiver *pushTestPeer, root string) (byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sender.host.Connect(ctx, peer.AddrInfo{ID: receiver.host.ID(), Addrs: receiver.host.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	stream, err := sender.host.NewStream(ctx, receiver.host.ID(), pushProtocol)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(15 * time.Second))
	set, err := sender.cs.loadContentSet(root)
	if err != nil {
		t.Fatalf("load set: %v", err)
	}
	return sender.cs.pushOnStream(stream, set)
}

func TestPushSingleBlobSet(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 1<<30)

	data := []byte("# a page worth replicating")
	root, err := sender.cs.Put(context.Background(), data)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	status, err := pushBetween(t, sender, receiver, root)
	if err != nil || status != pushAccept {
		t.Fatalf("push: status=%d err=%v", status, err)
	}
	got, err := receiver.cs.store.Get(root)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("receiver blob: err=%v", err)
	}
	if !receiver.cs.index.Has(root) {
		t.Fatalf("receiver did not index the set")
	}
	// A second push of the same set answers pushHave.
	status, err = pushBetween(t, sender, receiver, root)
	if err != nil || status != pushHave {
		t.Fatalf("re-push: status=%d err=%v", status, err)
	}
}

func TestPushChunkedSet(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 1<<30)

	data := testBytes(chunkSize + 999) // manifest + 2 chunks
	root, _, err := sender.cs.PutStream(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	status, err := pushBetween(t, sender, receiver, root)
	if err != nil || status != pushAccept {
		t.Fatalf("push: status=%d err=%v", status, err)
	}
	// The receiver can serve the whole content from its own store.
	got, err := receiver.cs.Fetch(context.Background(), root)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("receiver reassembly: err=%v equal=%v", err, bytes.Equal(got, data))
	}
}

func TestPushDeclinedOverBudget(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 100) // tiny hosting budget

	root, err := sender.cs.Put(context.Background(), testBytes(4096))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	status, err := pushBetween(t, sender, receiver, root)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if status != pushDecline {
		t.Fatalf("expected decline, got %d", status)
	}
	if receiver.cs.store.Has(root) || receiver.cs.index.Has(root) {
		t.Fatalf("declined content was stored anyway")
	}
}

// TestPushCorruptBlobRejected: a pusher whose bytes do not match the offered
// root must get a failed final ack and leave no index entry.
func TestPushCorruptBlobRejected(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 1<<30)

	data := []byte("honest bytes")
	root, _ := sender.cs.Put(context.Background(), data)
	// Corrupt the sender's stored blob after hashing (simulates a lying peer:
	// same package, so we can write the store file directly).
	if err := os.WriteFile(sender.cs.store.path(root), []byte("evil bytes"), 0600); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	status, err := pushBetween(t, sender, receiver, root)
	if status == pushAccept && err == nil {
		t.Fatalf("corrupt push reported success")
	}
	if receiver.cs.index.Has(root) {
		t.Fatalf("corrupt set indexed")
	}
}

// --- replicator decision logic (fakes, no network) ---

type fakeSwarm struct {
	peers   []peer.ID
	provs   []peer.ID
	accepts map[peer.ID]byte // default pushAccept
	pushed  []peer.ID
}

func (f *fakeSwarm) closest(ctx context.Context, root string, n int) ([]peer.ID, error) {
	return f.peers, nil
}
func (f *fakeSwarm) providers(ctx context.Context, root string, max int) ([]peer.ID, error) {
	return f.provs, nil
}
func (f *fakeSwarm) push(ctx context.Context, p peer.ID, root string) (byte, error) {
	f.pushed = append(f.pushed, p)
	if status, ok := f.accepts[p]; ok {
		return status, nil
	}
	return pushAccept, nil
}

func testPeerIDs(n int) []peer.ID {
	ids := make([]peer.ID, n)
	for i := range ids {
		ids[i] = peer.ID(string(rune('A' + i)))
	}
	return ids
}

func newFakeReplicator(f *fakeSwarm, self peer.ID, replicas int) *replicator {
	return &replicator{
		self:      self,
		replicas:  replicas,
		closest:   f.closest,
		providers: f.providers,
		push:      f.push,
	}
}

func TestReplicateSkipsSelfAndDecliners(t *testing.T) {
	ids := testPeerIDs(6)
	self := ids[0]
	f := &fakeSwarm{
		peers:   ids,
		accepts: map[peer.ID]byte{ids[1]: pushDecline},
	}
	r := newFakeReplicator(f, self, 3)

	placed := r.replicate(context.Background(), "roothash")
	if placed != 3 {
		t.Fatalf("placed %d, want 3", placed)
	}
	for _, p := range f.pushed {
		if p == self {
			t.Fatalf("pushed to self")
		}
	}
	// Decliner was tried but didn't count; 4 pushes total (1 decline + 3 accepts).
	if len(f.pushed) != 4 {
		t.Fatalf("pushed %d times, want 4", len(f.pushed))
	}
}

func TestHealNoopWhenEnoughProviders(t *testing.T) {
	ids := testPeerIDs(6)
	f := &fakeSwarm{peers: ids, provs: ids[1:4]} // 3 other providers + self = 4
	r := newFakeReplicator(f, ids[0], 3)

	if err := r.heal(context.Background(), "roothash"); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if len(f.pushed) != 0 {
		t.Fatalf("heal pushed despite full replica count")
	}
}

func TestHealTopsUpMissingReplicas(t *testing.T) {
	ids := testPeerIDs(8)
	self := ids[0]
	// Only one other provider alive: holders = 2, target = 4 -> need 2 pushes,
	// and they must go to peers that are not already providers.
	f := &fakeSwarm{peers: ids, provs: ids[1:2]}
	r := newFakeReplicator(f, self, 3)

	if err := r.heal(context.Background(), "roothash"); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if len(f.pushed) != 2 {
		t.Fatalf("heal pushed %d times, want 2", len(f.pushed))
	}
	for _, p := range f.pushed {
		if p == self || p == ids[1] {
			t.Fatalf("heal pushed to an existing holder %s", p)
		}
	}
}

func TestHealCountsHaveAsSuccess(t *testing.T) {
	ids := testPeerIDs(6)
	f := &fakeSwarm{
		peers:   ids,
		provs:   nil, // provider records expired, but peers still hold the data
		accepts: map[peer.ID]byte{ids[1]: pushHave, ids[2]: pushHave, ids[3]: pushHave},
	}
	r := newFakeReplicator(f, ids[0], 3)
	if err := r.heal(context.Background(), "roothash"); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if len(f.pushed) != 3 {
		t.Fatalf("heal pushed %d times, want exactly 3 (have counts as success)", len(f.pushed))
	}
}

func TestHealPropagatesProviderError(t *testing.T) {
	r := &replicator{
		self:     peer.ID("self"),
		replicas: 3,
		providers: func(ctx context.Context, root string, max int) ([]peer.ID, error) {
			return nil, errors.New("dht down")
		},
	}
	if err := r.heal(context.Background(), "roothash"); err == nil {
		t.Fatalf("expected provider error")
	}
}
