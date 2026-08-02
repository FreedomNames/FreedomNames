package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/content"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// claimTestPeer is a content service over a real store and index with no
// network. Transfers are driven blob by blob through the same claim API the
// push receiver uses, which is what lets a test interleave two of them at
// exact points instead of racing goroutines and hoping.
type claimTestPeer struct {
	cs  *ContentService
	dir string
}

func newClaimTestPeer(t *testing.T) *claimTestPeer {
	t.Helper()
	dir := t.TempDir()
	store, err := content.NewBlobStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ix, err := content.LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	cs := NewLocalContentService(store)
	cs.index = ix
	return &claimTestPeer{cs: cs, dir: dir}
}

func (p *claimTestPeer) hasBlob(t *testing.T, hash string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(p.dir, hash))
	return err == nil
}

// transfer models one writer assembling a set: it claims and stores blobs, and
// then either records the set or throws the whole thing away.
type transfer struct {
	t      *testing.T
	peer   *claimTestPeer
	claims *content.BlobClaims
	blobs  map[string][]byte
}

func (p *claimTestPeer) begin(t *testing.T) *transfer {
	return &transfer{t: t, peer: p, claims: p.cs.index.BeginWrite(), blobs: map[string][]byte{}}
}

// write claims a blob and puts it, in that order, exactly as handlePushStream
// and PutStream do.
func (tr *transfer) write(data []byte) string {
	tr.t.Helper()
	hash, err := content.ContentHash(data)
	if err != nil {
		tr.t.Fatalf("hash: %v", err)
	}
	tr.claims.Claim(hash)
	if _, err := tr.peer.cs.store.Put(data); err != nil {
		tr.t.Fatalf("put: %v", err)
	}
	tr.blobs[hash] = data
	return hash
}

// consume claims a blob this transfer depends on but did not write itself: the
// content-addressed store already holds those exact bytes.
func (tr *transfer) consume(hash string) {
	tr.t.Helper()
	if !tr.peer.hasBlob(tr.t, hash) {
		tr.t.Fatalf("consume %s: not in the store", hash)
	}
	tr.claims.Claim(hash)
}

func (tr *transfer) commit(root string, size int64, chunks []string) {
	tr.t.Helper()
	tr.peer.cs.index.AddHosted(root, size, chunks, "test", tr.claims)
}

func (tr *transfer) abort() { tr.claims.Discard() }

// The scenario the claim mechanism exists for: two transfers share a blob, one
// aborts while the other is still in flight. Before claims, the aborting side
// asked the index whether anything referenced the blob, got "no" because the
// other transfer had not recorded its set yet, and deleted bytes that the
// survivor went on to index. The survivor ended up with a set the index
// promised and the store could not serve.
func TestAbortedTransferKeepsBlobsAnotherTransferIsUsing(t *testing.T) {
	p := newClaimTestPeer(t)

	// A manifest describes at least two chunks, so B's set is the shared chunk
	// plus a tail of its own.
	const chunkSize = 4096
	shared := testsupport.TestBytes(chunkSize)
	tail := testsupport.TestBytes(1024)

	// 1. Transfer A writes the shared chunk and pauses.
	a := p.begin(t)
	x := a.write(shared)
	if !p.hasBlob(t, x) {
		t.Fatal("A did not store the shared blob")
	}

	// 2. Transfer B consumes it for a set of its own, and pauses.
	b := p.begin(t)
	b.consume(x)
	tailHash := b.write(tail)
	manifest := content.ChunkManifest{
		TotalSize: int64(len(shared) + len(tail)),
		ChunkSize: chunkSize,
		Chunks:    []string{x, tailHash},
	}
	encoded, err := content.EncodeManifest(&manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	bRoot := b.write(encoded)

	// 3. A aborts.
	a.abort()

	// 4. The shared blob must survive: B is still holding it.
	if !p.hasBlob(t, x) {
		t.Fatal("aborting transfer deleted a blob another in-flight transfer was using")
	}

	// 5. B completes, and the set it indexed has to actually be readable.
	b.commit(bRoot, int64(len(encoded))+manifest.TotalSize, manifest.Chunks)
	if !p.cs.index.Has(bRoot) {
		t.Fatal("committed set is not indexed")
	}
	got, err := p.cs.Fetch(context.Background(), bRoot)
	if err != nil {
		t.Fatalf("fetch the committed set: %v", err)
	}
	if want := string(shared) + string(tail); string(got) != want {
		t.Fatalf("committed set served %d bytes, want %d", len(got), len(want))
	}
}

// With nobody left holding it and no set naming it, the shared blob does go.
// The claim is a reprieve for as long as someone is using the bytes, not a
// licence to keep them forever.
func TestBlobGoesOnceEveryTransferHasAborted(t *testing.T) {
	p := newClaimTestPeer(t)
	shared := testsupport.TestBytes(2048)

	a := p.begin(t)
	x := a.write(shared)
	b := p.begin(t)
	b.consume(x)

	a.abort()
	if !p.hasBlob(t, x) {
		t.Fatal("blob removed while a transfer still held it")
	}
	b.abort()
	if p.hasBlob(t, x) {
		t.Fatal("blob survived after every transfer aborted")
	}
}

// An aborting transfer never touches bytes an indexed set depends on, however
// it came to be carrying them.
func TestAbortLeavesAnIndexedSharedBlobAlone(t *testing.T) {
	p := newClaimTestPeer(t)
	shared := testsupport.TestBytes(1024)

	// An already-recorded set owns the blob.
	keeper := p.begin(t)
	x := keeper.write(shared)
	keeper.commit(x, int64(len(shared)), nil)

	// A later transfer carrying the same bytes fails.
	doomed := p.begin(t)
	doomed.write(shared)
	doomed.abort()

	if !p.hasBlob(t, x) {
		t.Fatal("abort deleted a blob an indexed set references")
	}
	if _, err := p.cs.Fetch(context.Background(), x); err != nil {
		t.Fatalf("indexed set no longer readable: %v", err)
	}
}

// Eviction is the other place a blob can be deleted for looking unwanted, and
// it has to respect claims for the same reason a rollback does: a transfer part
// way through a set that shares bytes with the victim would otherwise have them
// taken away mid-flight.
func TestEvictionSparesBlobsATransferIsUsing(t *testing.T) {
	p := newClaimTestPeer(t)
	shared := testsupport.TestBytes(4096)
	size := int64(len(shared))

	// A hosted set holds the blob.
	victim := p.begin(t)
	x := victim.write(shared)
	victim.commit(x, size, nil)

	// A transfer starts depending on those same bytes.
	other := p.begin(t)
	other.consume(x)

	// Admit another set of the same size into a budget that only fits one, with
	// a clock far enough ahead that the resident set is TTL-expired and so has
	// lost its churn protection. That leaves eviction the only way to fit, and
	// the resident set the only candidate.
	if !p.cs.index.Admit(size, size, size, time.Second, time.Now().Add(48*time.Hour)) {
		t.Fatal("admission should have made room by evicting the resident set")
	}
	if p.cs.index.Has(x) {
		t.Fatal("the resident set was not evicted, so this proves nothing")
	}
	if !p.hasBlob(t, x) {
		t.Fatal("eviction deleted a blob an in-flight transfer was using")
	}

	// Once that transfer commits, the bytes are still there to be served.
	other.commit(x, size, nil)
	if _, err := p.cs.Fetch(context.Background(), x); err != nil {
		t.Fatalf("set committed after eviction is unreadable: %v", err)
	}
}

// The counterpart: with no claim held, eviction does reclaim the blob. Without
// this, the test above would pass just as happily against an eviction path that
// had stopped deleting anything at all.
func TestEvictionStillReclaimsUnclaimedBlobs(t *testing.T) {
	p := newClaimTestPeer(t)
	shared := testsupport.TestBytes(4096)
	size := int64(len(shared))

	victim := p.begin(t)
	x := victim.write(shared)
	victim.commit(x, size, nil)

	if !p.cs.index.Admit(size, size, size, time.Second, time.Now().Add(48*time.Hour)) {
		t.Fatal("admission should have made room by evicting the resident set")
	}
	if p.hasBlob(t, x) {
		t.Fatal("eviction left the blob behind even though nothing claimed it")
	}
}
