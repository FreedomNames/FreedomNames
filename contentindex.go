package main

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
	StoredAt   int64    `json:"storedAt"`   // unix seconds
	LastAccess int64    `json:"lastAccess"` // unix seconds, TTL + LRU driver
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
		path:  filepath.Join(dir, indexFileName),
		store: store,
		sets:  make(map[string]*contentMeta),
		blobs: make(map[string]map[string]bool),
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
		if m, ok := decodeManifest(data); ok {
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
		if _, size, err := store.Open(h); err == nil {
			adopt(h, size, nil)
		}
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

// MarkOwned records (or upgrades) a set as this node's own published content.
func (ix *ContentIndex) MarkOwned(root string, size int64, chunks []string) {
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
	ix.save()
}

// AddHosted records a set stored on behalf of the network (a push or a cached
// fetch). It does not check the budget — call Admit first.
func (ix *ContentIndex) AddHosted(root string, size int64, chunks []string, from string) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
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
	var total int64
	for _, m := range ix.sets {
		if !m.Owned {
			total += m.Size
		}
	}
	return total
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
	if size > maxSize || size > budget {
		return false
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()

	for ix.hostedBytesLocked()+size > budget {
		victim := ""
		victimExpired := false
		var oldest int64
		for root, m := range ix.sets {
			if m.Owned {
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
		ix.evictLocked(victim)
	}
	return true
}

// evictLocked removes a set: index entry plus every blob no other set still
// references. Caller holds mu.
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
			ix.store.Delete(h)
		}
	}
	release(root)
	for _, ch := range m.Chunks {
		release(ch)
	}
	ix.save()
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
