package content

import (
	"testing"
	"time"
)

func newTestIndex(t *testing.T) (*ContentIndex, *BlobStore, string) {
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
	return ix, store, dir
}

func TestIndexPersistenceRoundTrip(t *testing.T) {
	ix, store, dir := newTestIndex(t)
	h, _ := store.Put([]byte("owned page"))
	ix.MarkOwned(h, 10, nil)
	h2, _ := store.Put([]byte("hosted page"))
	ix.AddHosted(h2, 11, nil, "12D3KooTestPeer")

	reloaded, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	if m := reloaded.sets[h]; m == nil || !m.Owned {
		t.Fatalf("owned set lost on reload: %+v", m)
	}
	if m := reloaded.sets[h2]; m == nil || m.Owned || m.From != "12D3KooTestPeer" {
		t.Fatalf("hosted set lost on reload: %+v", m)
	}
}

// TestIndexReconcileAdoptsOrphans stores a chunked content set plus a lone
// blob with no index at all, and checks reconciliation adopts the manifest and
// its chunks as ONE hosted set, and the lone blob as another.
func TestIndexReconcileAdoptsOrphans(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewBlobStore(dir)

	// Lay down a chunked set the way PutStream would: the chunk blobs, then a
	// manifest blob listing them. Built from this package's own primitives so
	// the test stays inside internal/content (the chunk-writing path itself is
	// covered by the node package's PutStream tests).
	data := testBytes(ChunkSize + 500)
	c0, err := store.Put(data[:ChunkSize])
	if err != nil {
		t.Fatalf("put chunk 0: %v", err)
	}
	c1, err := store.Put(data[ChunkSize:])
	if err != nil {
		t.Fatalf("put chunk 1: %v", err)
	}
	manifest, err := EncodeManifest(&ChunkManifest{
		TotalSize: int64(len(data)),
		ChunkSize: ChunkSize,
		Chunks:    []string{c0, c1},
	})
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	root, err := store.Put(manifest)
	if err != nil {
		t.Fatalf("put manifest: %v", err)
	}
	lone, _ := store.Put([]byte("a lone page"))

	ix, err := LoadContentIndex(dir, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	m := ix.sets[root]
	if m == nil || m.Owned || len(m.Chunks) != 2 {
		t.Fatalf("manifest set not adopted correctly: %+v", m)
	}
	if ix.sets[lone] == nil {
		t.Fatalf("lone blob not adopted")
	}
	// Chunks must be claimed by the manifest set, not adopted separately.
	if len(ix.sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(ix.sets))
	}
}

func TestIndexAdmitBudgetAndLRU(t *testing.T) {
	ix, store, _ := newTestIndex(t)
	now := time.Now()
	old := now.Add(-2 * time.Hour) // past the eviction protection window

	put := func(content string) string {
		h, _ := store.Put([]byte(content))
		return h
	}
	owned := put("owned content")
	ix.MarkOwned(owned, 400, nil)
	a, b := put("hosted a"), put("hosted b")
	ix.AddHosted(a, 300, nil, "")
	ix.AddHosted(b, 300, nil, "")
	// Backdate stored/access times so LRU + protection logic is exercised.
	ix.mu.Lock()
	ix.sets[a].StoredAt, ix.sets[a].LastAccess = old.Unix(), old.Unix()
	ix.sets[b].StoredAt, ix.sets[b].LastAccess = old.Unix(), old.Add(30*time.Minute).Unix()
	ix.mu.Unlock()

	// Budget 1000, hosted 600: a 300-byte set fits without eviction.
	if !ix.Admit(300, 1000, MaxContentSize, 24*time.Hour, now) {
		t.Fatalf("admit within budget refused")
	}
	// A 500-byte set needs 100 freed: LRU evicts a (least recently accessed).
	if !ix.Admit(500, 1000, MaxContentSize, 24*time.Hour, now) {
		t.Fatalf("admit with eviction refused")
	}
	if ix.Has(a) {
		t.Fatalf("LRU should have evicted a")
	}
	if store.Has(a) {
		t.Fatalf("evicted set's blob still on disk")
	}
	if !ix.Has(b) || !ix.Has(owned) {
		t.Fatalf("wrong set evicted")
	}
	// A budget-sized ask may evict every hosted set, but never an owned one.
	if !ix.Admit(1000, 1000, MaxContentSize, 24*time.Hour, now.Add(2*time.Hour)) {
		t.Fatalf("budget-sized admit refused despite evictable hosted sets")
	}
	if ix.Has(b) {
		t.Fatalf("hosted b should have been evicted for the budget-sized ask")
	}
	if !ix.Has(owned) || !store.Has(owned) {
		t.Fatalf("owned set was evicted")
	}
	// An ask beyond the budget is refused outright, owned still intact.
	if ix.Admit(1100, 1000, MaxContentSize, 24*time.Hour, now) {
		t.Fatalf("admit over budget accepted")
	}
	// Over the per-set cap is refused outright.
	if ix.Admit(50, 1000, 10, 24*time.Hour, now) {
		t.Fatalf("admit over per-set cap")
	}
}

// TestIndexExpiredKeptUntilSpaceNeeded: TTL never deletes data on its own —
// an expired set survives while the budget has room, and is merely the FIRST
// eviction candidate once space is actually needed.
func TestIndexExpiredKeptUntilSpaceNeeded(t *testing.T) {
	ix, store, _ := newTestIndex(t)
	now := time.Now()

	stale, _ := store.Put([]byte("stale hosted content"))
	fresh, _ := store.Put([]byte("fresh hosted content"))
	ix.AddHosted(stale, 400, nil, "")
	ix.AddHosted(fresh, 400, nil, "")
	ix.mu.Lock()
	old := now.Add(-48 * time.Hour).Unix()
	ix.sets[stale].StoredAt, ix.sets[stale].LastAccess = old, old
	// fresh: recently accessed but stored long ago (not protection-shielded),
	// so only the TTL priority distinguishes it from stale.
	ix.sets[fresh].StoredAt = old
	ix.mu.Unlock()

	// Plenty of room: the expired set must NOT be removed.
	if !ix.Admit(100, 1000, MaxContentSize, 24*time.Hour, now) {
		t.Fatalf("admit refused despite free budget")
	}
	if !ix.Has(stale) || !store.Has(stale) {
		t.Fatalf("expired set was deleted while budget had room")
	}

	// Budget pressure: the expired set is evicted first, the live one stays,
	// even though eviction of either would have made enough room.
	if !ix.Admit(400, 1000, MaxContentSize, 24*time.Hour, now) {
		t.Fatalf("admit under pressure refused")
	}
	if ix.Has(stale) || store.Has(stale) {
		t.Fatalf("expired set should have been evicted first (index=%v blob=%v)", ix.Has(stale), store.Has(stale))
	}
	if !ix.Has(fresh) || !store.Has(fresh) {
		t.Fatalf("live set evicted while an expired one existed")
	}
}

// TestIndexSharedChunkSurvivesEviction: two sets sharing a blob; evicting one
// must not delete the shared blob.
func TestIndexSharedChunkSurvivesEviction(t *testing.T) {
	ix, store, _ := newTestIndex(t)
	shared, _ := store.Put([]byte("shared chunk"))
	rootA, _ := store.Put([]byte("root a"))
	rootB, _ := store.Put([]byte("root b"))
	old := time.Now().Add(-3 * time.Hour).Unix()
	ix.AddHosted(rootA, 200, []string{shared}, "")
	ix.AddHosted(rootB, 200, []string{shared}, "")
	ix.mu.Lock()
	ix.sets[rootA].StoredAt, ix.sets[rootA].LastAccess = old, old
	ix.mu.Unlock()

	// Force eviction of rootA only.
	if !ix.Admit(250, 450, MaxContentSize, 240*time.Hour, time.Now()) {
		t.Fatalf("admit refused")
	}
	if ix.Has(rootA) {
		t.Fatalf("rootA should be evicted")
	}
	if !store.Has(shared) {
		t.Fatalf("shared blob deleted while rootB still references it")
	}
	if store.Has(rootA) {
		t.Fatalf("rootA blob should be deleted")
	}
}

func TestIndexTouchBlobRefreshesSet(t *testing.T) {
	ix, store, _ := newTestIndex(t)
	chunk, _ := store.Put([]byte("chunk"))
	root, _ := store.Put([]byte("root"))
	ix.AddHosted(root, 100, []string{chunk}, "")
	ix.mu.Lock()
	ix.sets[root].LastAccess = 12345
	ix.mu.Unlock()

	ix.TouchBlob(chunk)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.sets[root].LastAccess == 12345 {
		t.Fatalf("TouchBlob did not refresh the owning set")
	}
}
