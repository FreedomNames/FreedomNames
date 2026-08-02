package node

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/content"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// storeBytes reports how many bytes of blobs sit in a store directory,
// ignoring the index sidecar. It measures what a push actually cost the
// receiver's disk, which is the thing the hosting budget is supposed to bound.
func storeBytes(t *testing.T, dir string) int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	var total int64
	for _, e := range entries {
		if e.Name() == "index.json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}

// openPushStream connects sender to receiver and opens a raw push stream, so a
// test can drive the wire protocol by hand instead of going through
// pushOnStream (which is always honest about what it offers).
func openPushStream(t *testing.T, sender, receiver *pushTestPeer) network.Stream {
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
	t.Cleanup(func() { stream.Close() })
	stream.SetDeadline(time.Now().Add(15 * time.Second))
	return stream
}

// A push offer is what the hosting policy admits, so it also has to bound what
// the transfer may deliver. A sender that understates its offer used to get a
// token-sized reservation and then stream the real payload anyway: the final
// size check rejected the set, but the blobs were already on disk, counted
// against nothing and evicted by nothing.
func TestUnderstatedPushOfferCannotExceedTheBudget(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 8<<10) // 8 KiB hosting budget

	payload := testsupport.TestBytes(1 << 20) // 1 MiB, far over that budget
	root, err := sender.cs.Put(context.Background(), payload)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	set, err := sender.cs.loadContentSet(root)
	if err != nil {
		t.Fatalf("load set: %v", err)
	}

	stream := openPushStream(t, sender, receiver)
	// The lie: offer one byte, then stream the whole megabyte.
	if err := writePushOffer(stream, set.root, 1, set.numBlobs()); err != nil {
		t.Fatalf("offer: %v", err)
	}
	status, err := readStatusByte(stream)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status == pushAccept {
		rc, size, err := sender.cs.store.Open(set.root)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		// A receiver that refuses the blob on its length header stops reading,
		// so this write is expected to stall; a short deadline keeps the test
		// from waiting out the full stream timeout for that.
		stream.SetDeadline(time.Now().Add(2 * time.Second))
		writeRequest(stream, set.root)
		writeBlobHeader(stream, uint64(size))
		io.Copy(stream, rc)
		rc.Close()
		if final, err := readStatusByte(stream); err == nil && final == 1 {
			t.Fatal("receiver acked an understated push as stored")
		}
	}

	if onDisk := storeBytes(t, receiver.cs.store.Dir()); onDisk > 8<<10 {
		t.Errorf("%d bytes landed on disk against an 8 KiB hosting budget", onDisk)
	}
	if receiver.cs.index.Has(root) {
		t.Error("understated set was indexed")
	}
	if receiver.cs.index.HostedBytes() > 8<<10 {
		t.Errorf("hosted bytes %d exceed the budget", receiver.cs.index.HostedBytes())
	}
}

// A push that dies part way through leaves nothing behind. Without the
// rollback the delivered prefix stayed in the store: unindexed, so invisible to
// the budget and to eviction, and reclaimed only if the node happened to
// restart.
func TestAbandonedPushLeavesNoBlobsBehind(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 1<<30)

	// Chunked content, so the transfer has several blobs and can be cut off
	// after the receiver has already stored some of them.
	payload := testsupport.TestBytes(3 * content.ChunkSize)
	root, _, err := sender.cs.PutStream(context.Background(), newRepeatReader(payload))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	set, err := sender.cs.loadContentSet(root)
	if err != nil {
		t.Fatalf("load set: %v", err)
	}
	if len(set.chunks) < 2 {
		t.Fatalf("expected a chunked set, got %d chunks", len(set.chunks))
	}

	stream := openPushStream(t, sender, receiver)
	if err := writePushOffer(stream, set.root, uint64(set.size), set.numBlobs()); err != nil {
		t.Fatalf("offer: %v", err)
	}
	if status, err := readStatusByte(stream); err != nil || status != pushAccept {
		t.Fatalf("expected accept, got %d (%v)", status, err)
	}
	// Send the manifest and exactly one chunk, then hang up.
	for _, h := range []string{set.root, set.chunks[0]} {
		rc, size, err := sender.cs.store.Open(h)
		if err != nil {
			t.Fatalf("open %s: %v", h, err)
		}
		writeRequest(stream, h)
		writeBlobHeader(stream, uint64(size))
		io.Copy(stream, rc)
		rc.Close()
	}
	stream.Close()

	// The receiver's handler notices the truncated stream and rolls back. Give
	// it a moment to run, since it is on the other side of a real connection.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if storeBytes(t, receiver.cs.store.Dir()) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if onDisk := storeBytes(t, receiver.cs.store.Dir()); onDisk != 0 {
		t.Errorf("abandoned push left %d bytes on disk", onDisk)
	}
	if receiver.cs.index.Has(root) {
		t.Error("abandoned set was indexed")
	}
}

// Rollback must not take blobs the node holds for another set. Content
// addressing means an aborted push can carry a blob a legitimate set depends
// on, and availability comes first: a replica the network is counting on is
// never collateral damage of someone else's failed transfer.
func TestRollbackSparesSharedBlobs(t *testing.T) {
	sender := newPushTestPeer(t, 1<<30)
	receiver := newPushTestPeer(t, 1<<30)

	shared := []byte("bytes two different sets both contain")
	// The receiver already hosts this blob as a set of its own.
	root, err := receiver.cs.Put(context.Background(), shared)
	if err != nil {
		t.Fatalf("receiver put: %v", err)
	}
	receiver.cs.index.AddHosted(root, int64(len(shared)), nil, "someone")

	// The sender offers the same bytes but lies about the size, so the push is
	// rejected and rolled back.
	if _, err := sender.cs.Put(context.Background(), shared); err != nil {
		t.Fatalf("sender put: %v", err)
	}
	stream := openPushStream(t, sender, receiver)
	if err := writePushOffer(stream, root, uint64(len(shared)+1), 1); err != nil {
		t.Fatalf("offer: %v", err)
	}
	status, _ := readStatusByte(stream)
	if status == pushAccept {
		writeRequest(stream, root)
		writeBlobHeader(stream, uint64(len(shared)))
		stream.Write(shared)
		readStatusByte(stream)
	}
	stream.Close()

	time.Sleep(200 * time.Millisecond)
	if !receiver.cs.store.Has(root) {
		t.Fatal("rollback deleted a blob another set references")
	}
}

// repeatReader serves a fixed payload once, as an io.Reader, without pulling
// PutStream's chunking path into bytes.Reader-specific behaviour.
type repeatReader struct {
	data []byte
	off  int
}

func newRepeatReader(data []byte) *repeatReader { return &repeatReader{data: data} }

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
