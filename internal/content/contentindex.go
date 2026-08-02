package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// This file tracks WHAT the content store holds and WHY: each entry is a
// "content set" (a single blob, or a manifest plus its chunks) keyed by its
// root hash. Sets are either owned (published via this node — never evicted)
// or hosted (pushed to us or cached from a fetch — counted against the
// operator's hosting budget, expired by TTL, evicted LRU under pressure).
// The index is a sidecar JSON file next to the blobs, written atomically.

// indexFileName is the sidecar file inside the content directory.
const indexFileName = "index.json"

// evictionProtection is how long a freshly stored hosted set is safe from LRU
// eviction, so two large sets cannot thrash-evict each other.
const evictionProtection = time.Hour

// manifestProbeLimit caps how large an unindexed blob may be to be probed as a
// manifest during reconciliation (real manifests are a few KB).
const manifestProbeLimit = 1 << 20

// contentMeta describes one content set.
type contentMeta struct {
	Owned      bool     `json:"owned"`
	Size       int64    `json:"size"` // total bytes: root blob + chunks
	Chunks     []string `json:"chunks,omitempty"`
	StoredAt   int64    `json:"storedAt"`       // unix seconds
	LastAccess int64    `json:"lastAccess"`     // unix seconds, TTL + LRU driver
	From       string   `json:"from,omitempty"` // pusher peer ID (future per-peer caps)
}

// ContentIndex is the in-memory index with its on-disk sidecar.
type ContentIndex struct {
	path  string
	store *BlobStore

	mu    sync.Mutex
	sets  map[string]*contentMeta
	blobs map[string]map[string]bool // blob hash -> set roots referencing it
	dirty bool                       // lastAccess-only changes awaiting a flush

	// pending counts in-flight claims per blob hash: writers that are part way
	// through assembling a set and are relying on that blob, before any index
	// entry points at it. Without it, "no set references this blob" reads as
	// "nothing wants this blob", which is false for the entire duration of
	// every transfer. See BlobClaims.
	pending map[string]int

	// reserved is the bytes promised to admitted-but-not-yet-stored sets. An
	// inbound push is admitted before its bytes arrive, so without counting the
	// promise, N concurrent pushes each measure the same pre-transfer usage and
	// all pass — overshooting the operator's budget by a factor of N.
	reserved int64
}

// indexFile is the on-disk shape.
type indexFile struct {
	V    int                     `json:"v"`
	Sets map[string]*contentMeta `json:"sets"`
}

// LoadContentIndex reads (or initializes) the index for a store and reconciles
// it against the blobs actually on disk: entries whose root blob vanished are
// dropped, and unindexed blobs are adopted as hosted sets with a fresh TTL —
// so stores from before the index existed keep working.
func LoadContentIndex(dir string, store *BlobStore) (*ContentIndex, error) {
	ix := &ContentIndex{
		path:    filepath.Join(dir, indexFileName),
		store:   store,
		sets:    make(map[string]*contentMeta),
		blobs:   make(map[string]map[string]bool),
		pending: make(map[string]int),
	}
	if data, err := os.ReadFile(ix.path); err == nil {
		var f indexFile
		if err := json.Unmarshal(data, &f); err == nil && f.Sets != nil {
			ix.sets = f.Sets
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	onDisk, err := store.List()
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(onDisk))
	for _, h := range onDisk {
		present[h] = true
	}

	// Drop entries whose root blob is gone; rebuild the reverse map.
	for root, m := range ix.sets {
		if !present[root] {
			delete(ix.sets, root)
			continue
		}
		ix.reference(root, m)
	}

	// Adopt unindexed blobs: manifests first (claiming their chunks), then any
	// leftovers as single-blob sets. All adopted as hosted with a full TTL.
	now := time.Now().Unix()
	adopt := func(root string, size int64, chunks []string) {
		m := &contentMeta{Size: size, Chunks: chunks, StoredAt: now, LastAccess: now}
		ix.sets[root] = m
		ix.reference(root, m)
	}
	for _, h := range onDisk {
		if len(ix.blobs[h]) > 0 {
			continue
		}
		rc, size, err := store.Open(h)
		if err != nil {
			continue
		}
		rc.Close()
		if size > manifestProbeLimit {
			continue // decided below as a plain blob
		}
		data, err := store.Get(h)
		if err != nil {
			continue
		}
		if m, ok := DecodeManifest(data); ok {
			complete := true
			for _, ch := range m.Chunks {
				if !present[ch] {
					complete = false
					break
				}
			}
			if complete {
				adopt(h, size+m.TotalSize, m.Chunks)
			}
		}
	}
	for _, h := range onDisk {
		if len(ix.blobs[h]) > 0 {
			continue
		}
		// Close the handle: without it, adopting a store full of unindexed
		// blobs leaks one open file descriptor per blob and can exhaust the
		// process limit before the node has finished starting.
		rc, size, err := store.Open(h)
		if err != nil {
			continue
		}
		rc.Close()
		adopt(h, size, nil)
	}

	if err := ix.save(); err != nil {
		return nil, err
	}
	return ix, nil
}

// reference records root's claim on its own blob and its chunks. Caller holds
// mu (or is single-threaded during load).
func (ix *ContentIndex) reference(root string, m *contentMeta) {
	claim := func(h string) {
		if ix.blobs[h] == nil {
			ix.blobs[h] = make(map[string]bool)
		}
		ix.blobs[h][root] = true
	}
	claim(root)
	for _, ch := range m.Chunks {
		claim(ch)
	}
}

// MarkOwned records (or upgrades) a set as this node's own published content,
// releasing the writer's claims on its blobs as it does so. Passing the claims
// in rather than releasing them afterwards keeps the two atomic: the set is
// never observable as recorded-but-unclaimed, nor as claimed-but-unrecorded.
// claims may be nil when the caller held none.
func (ix *ContentIndex) MarkOwned(root string, size int64, chunks []string, claims *BlobClaims) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	now := time.Now().Unix()
	m := ix.sets[root]
	if m == nil {
		m = &contentMeta{StoredAt: now}
		ix.sets[root] = m
	}
	m.Owned = true
	m.Size = size
	m.Chunks = chunks
	m.LastAccess = now
	ix.reference(root, m)
	// References now cover these blobs, so the claims go without deleting.
	ix.releaseClaimsLocked(claims, false)
	ix.save()
}

// AddHosted records a set stored on behalf of the network (a push or a cached
// fetch), releasing the writer's claims on its blobs in the same critical
// section. It does not check the budget — call Admit first. claims may be nil.
func (ix *ContentIndex) AddHosted(root string, size int64, chunks []string, from string, claims *BlobClaims) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.addHostedLocked(root, size, chunks, from)
	ix.releaseClaimsLocked(claims, false)
}

func (ix *ContentIndex) addHostedLocked(root string, size int64, chunks []string, from string) {
	if _, exists := ix.sets[root]; exists {
		ix.touchLocked(root)
		return
	}
	now := time.Now().Unix()
	m := &contentMeta{Size: size, Chunks: chunks, StoredAt: now, LastAccess: now, From: from}
	ix.sets[root] = m
	ix.reference(root, m)
	ix.save()
}

// Has reports whether the index tracks a set rooted at root.
func (ix *ContentIndex) Has(root string) bool {
	if ix == nil {
		return false
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	_, ok := ix.sets[root]
	return ok
}

// BlobClaims is one writer's in-flight hold on the blobs it is assembling into
// a set. It exists because "no set references this blob" is not the same
// question as "nothing wants this blob": between the moment a transfer writes a
// blob and the moment its set is indexed, the index knows nothing about it, and
// content addressing means a second transfer may be depending on those exact
// bytes at the same time.
//
// Consulting the index without this is a race no amount of locking fixes.
// Suppose transfer A writes chunk X, then transfer B starts a different set
// that also contains X. If A then aborts, the index still has no entry naming
// X, so A concludes nothing wants it and deletes it. B goes on to index a set
// whose bytes are no longer on disk. Widening the lock only decides which side
// of the gap the deletion lands on; it does not close it.
//
// So a writer claims each blob before it writes or relies on it, and a blob is
// removed only once no claim and no index reference remains. The claim is
// released as part of the same critical section that records the set, so a
// commit is never visible with its claims already gone.
//
// The zero value is unusable; call ContentIndex.BeginWrite.
type BlobClaims struct {
	ix     *ContentIndex
	hashes []string
}

// BeginWrite opens a claim set for one transfer. A nil index (the store-only
// service used in tests) yields a handle whose methods do nothing, so callers
// need no special case.
func (ix *ContentIndex) BeginWrite() *BlobClaims {
	return &BlobClaims{ix: ix}
}

// Claim registers an in-flight claim on a blob. Call it BEFORE writing the blob
// or before depending on one already in the store, so no window exists in which
// the blob is present but unspoken for.
//
// Claiming the same hash twice is fine: claims are counted, and each is
// released once.
func (c *BlobClaims) Claim(hash string) {
	if c == nil || c.ix == nil {
		return
	}
	c.ix.mu.Lock()
	defer c.ix.mu.Unlock()
	c.ix.pending[hash]++
	c.hashes = append(c.hashes, hash)
}

// Discard releases every claim and removes the blobs that nothing else wants:
// no other writer holding a claim, no indexed set referencing them. Use it when
// a transfer fails or is abandoned.
//
// Discarding after the set has been recorded is a no-op, so a deferred Discard
// is a safe way to guarantee an abandoned transfer cleans up after itself.
func (c *BlobClaims) Discard() {
	if c == nil || c.ix == nil {
		return
	}
	c.ix.mu.Lock()
	defer c.ix.mu.Unlock()
	c.ix.releaseClaimsLocked(c, true)
}

// releaseClaimsLocked drops a writer's claims, deleting any blob left wanted by
// nobody when discard is set. Caller holds mu.
//
// The handle is emptied, which is what makes a later Discard a no-op and keeps
// the counts honest if one is called twice.
func (ix *ContentIndex) releaseClaimsLocked(c *BlobClaims, discard bool) {
	if c == nil || c.ix == nil {
		return
	}
	for _, h := range c.hashes {
		remaining := ix.pending[h] - 1
		if remaining > 0 {
			ix.pending[h] = remaining
		} else {
			delete(ix.pending, h)
		}
		// Deleting only when both counts are zero is the whole invariant: a
		// blob survives while any writer is still assembling a set around it,
		// and while any recorded set names it.
		if discard && remaining <= 0 && len(ix.blobs[h]) == 0 {
			ix.store.Delete(h)
		}
	}
	c.hashes = nil
}

// Touch refreshes a set's last access time (TTL + LRU signal).
func (ix *ContentIndex) Touch(root string) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.touchLocked(root)
}

// TouchBlob refreshes every set that contains the blob (a chunk fetch keeps
// its whole set alive).
func (ix *ContentIndex) TouchBlob(hash string) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for root := range ix.blobs[hash] {
		ix.touchLocked(root)
	}
}

func (ix *ContentIndex) touchLocked(root string) {
	if m := ix.sets[root]; m != nil {
		m.LastAccess = time.Now().Unix()
		ix.dirty = true
	}
}

// Roots returns a snapshot of all set roots (the heal loop's work list).
func (ix *ContentIndex) Roots() []string {
	if ix == nil {
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	roots := make([]string, 0, len(ix.sets))
	for r := range ix.sets {
		roots = append(roots, r)
	}
	return roots
}

// HostedBytes returns the total size of hosted (non-owned) sets.
func (ix *ContentIndex) HostedBytes() int64 {
	if ix == nil {
		return 0
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.hostedBytesLocked()
}

func (ix *ContentIndex) hostedBytesLocked() int64 {
	total := ix.reserved
	for _, m := range ix.sets {
		if !m.Owned {
			total += m.Size
		}
	}
	return total
}

// Reserve holds size bytes of the budget for a transfer that has not arrived
// yet, and reports whether the set could be stored once it has. Every
// successful Reserve must be paired with exactly one Release or CommitHosted,
// or the budget leaks.
//
// Reserve DELETES NOTHING. A push offer is a few dozen bytes from any peer that
// cares to send one, and nothing obliges that peer to follow it with a transfer
// — so if an offer alone were allowed to evict, a stranger could wipe this
// node's hosted content for free, one abandoned offer at a time, without ever
// uploading anything. It only asks whether the eviction policy *could* make
// room; the deletion happens in CommitHosted, once the bytes are real.
//
// The test and the reservation happen under a single lock hold. Doing them as
// two steps would leave exactly the gap this exists to close: both callers test
// against the same usage, then both reserve.
func (ix *ContentIndex) Reserve(size, budget, maxSize int64, ttl time.Duration, now time.Time) bool {
	if ix == nil {
		return true // store-only service (tests): no policy
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if !ix.fitLocked(size, budget, maxSize, ttl, now, false) {
		return false
	}
	ix.reserved += size
	return true
}

// Release drops a reservation taken by Reserve whose transfer never completed.
// A completed one is consumed by CommitHosted instead.
func (ix *ContentIndex) Release(size int64) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.releaseLocked(size)
}

func (ix *ContentIndex) releaseLocked(size int64) {
	ix.reserved -= size
	if ix.reserved < 0 {
		ix.reserved = 0
	}
}

// CommitHosted turns a reservation into a stored set: it makes room for real,
// evicting if the budget requires it, and records the set. This is the only
// path on which an inbound push may cost this node content it already holds,
// and it is reached only after every blob has arrived and verified.
//
// It consumes the reservation whether or not it succeeds, so the caller must
// not also Release. It returns false if the budget filled while the transfer
// was in flight, in which case the set is not recorded.
//
// On success the writer's claims are released here, under the same lock that
// records the set, so the blobs pass straight from being claimed to being
// referenced with no instant in between where a concurrent Discard could see
// them as wanted by nobody. On failure the claims are left alone: the caller
// still holds them and is expected to Discard, which is what removes the bytes.
func (ix *ContentIndex) CommitHosted(root string, size int64, chunks []string, from string, budget, maxSize int64, ttl time.Duration, now time.Time, claims *BlobClaims) bool {
	if ix == nil {
		return true // store-only service (tests): no policy
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	// Drop the promise before measuring, or these bytes are counted twice and
	// the eviction pass frees twice what it needs to.
	ix.releaseLocked(size)
	if !ix.fitLocked(size, budget, maxSize, ttl, now, true) {
		return false
	}
	ix.addHostedLocked(root, size, chunks, from)
	ix.releaseClaimsLocked(claims, false)
	return true
}

// Admit decides whether a hosted set of the given size may be stored. Nothing
// is ever deleted while the budget has room — a TTL-expired set is NOT
// removed for being old, it just loses its protection and becomes the first
// eviction candidate when space is actually needed. Under pressure, eviction
// order is: expired hosted sets first (least recently accessed first), then
// LRU among the rest. Owned sets are never candidates; a freshly stored
// hosted set is protected for evictionProtection to prevent churn thrash.
// Returns false when the set cannot fit even after eviction.
func (ix *ContentIndex) Admit(size, budget, maxSize int64, ttl time.Duration, now time.Time) bool {
	if ix == nil {
		return true // store-only service (tests): no policy
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.fitLocked(size, budget, maxSize, ttl, now, true)
}

// fitLocked reports whether a hosted set of the given size fits, evicting to
// make room when apply is set. With apply false it is a dry run: it walks the
// same eviction order and answers the same question, but deletes nothing (see
// Reserve for why that distinction is load-bearing). Caller holds mu.
func (ix *ContentIndex) fitLocked(size, budget, maxSize int64, ttl time.Duration, now time.Time, apply bool) bool {
	if size > maxSize || size > budget {
		return false
	}

	evicted := false
	defer func() {
		// One write for the whole eviction pass rather than one per victim:
		// the index is rewritten in full each time it is saved.
		if evicted {
			ix.save()
		}
	}()

	// Dry runs cannot delete, so they track what they would have freed instead.
	var freed int64
	var spared map[string]bool

	for ix.hostedBytesLocked()-freed+size > budget {
		victim := ""
		victimExpired := false
		var oldest int64
		for root, m := range ix.sets {
			if m.Owned || spared[root] {
				continue
			}
			expired := ttl > 0 && now.Unix()-m.LastAccess > int64(ttl.Seconds())
			if !expired && now.Unix()-m.StoredAt < int64(evictionProtection.Seconds()) {
				continue // young, live sets are protected from churn
			}
			better := victim == "" ||
				(expired && !victimExpired) ||
				(expired == victimExpired && m.LastAccess < oldest)
			if better {
				victim, victimExpired, oldest = root, expired, m.LastAccess
			}
		}
		if victim == "" {
			return false
		}
		if apply {
			ix.evictLocked(victim)
			evicted = true
			continue
		}
		freed += ix.sets[victim].Size
		if spared == nil {
			spared = make(map[string]bool)
		}
		spared[victim] = true
	}
	return true
}

// evictLocked removes a set: index entry plus every blob no other set still
// references and no writer is still claiming. Caller holds mu and is
// responsible for persisting the change.
//
// The claim check matters as much here as it does on the rollback path: a
// transfer part way through assembling a set that happens to share bytes with
// the victim would otherwise have those bytes evicted out from under it. When a
// claim does hold the blob back, the index entry still goes; whichever writer
// still holds it decides its fate when it finishes.
func (ix *ContentIndex) evictLocked(root string) {
	m := ix.sets[root]
	if m == nil {
		return
	}
	delete(ix.sets, root)
	release := func(h string) {
		refs := ix.blobs[h]
		delete(refs, root)
		if len(refs) == 0 {
			delete(ix.blobs, h)
			if ix.pending[h] == 0 {
				ix.store.Delete(h)
			}
		}
	}
	release(root)
	for _, ch := range m.Chunks {
		release(ch)
	}
}

// Flush persists pending lastAccess updates (called opportunistically from the
// heal loop; structural changes save immediately).
func (ix *ContentIndex) Flush() {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.dirty {
		ix.save()
	}
}

// save writes the index atomically (temp + rename, like BlobStore.Put).
// Caller holds mu (or is single-threaded during load).
func (ix *ContentIndex) save() error {
	data, err := json.Marshal(indexFile{V: 1, Sets: ix.sets})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(ix.path), ".index-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, ix.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	ix.dirty = false
	return nil
}
