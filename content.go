package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/multiformats/go-base36"
	mh "github.com/multiformats/go-multihash"
)

// This file implements the content layer's local store: a content-addressed
// blobstore on disk. A blob's address is the base36-encoded sha2-256 multihash
// of its bytes — the same hash primitive used for pubKeyID (record.go), so a
// CONTENT record value is directly a content hash. The DHT stores only names
// and provider records; the actual page bytes live here and travel over the
// content stream protocol (contentnet.go).

// maxBlobSize caps a single blob (32 MiB) — the unit of storage and transfer.
// Content larger than one chunk is split into chunkSize blobs plus a manifest
// blob listing them (see ChunkManifest), so no single blob ever nears this cap;
// it remains the wire-level safety limit.
const maxBlobSize = 32 << 20

// chunkSize is the fixed size of each chunk of large content (8 MiB). Content
// up to chunkSize is stored as a single blob whose hash is the content hash
// (unchanged from the pre-chunking format); anything larger becomes
// ceil(size/chunkSize) chunk blobs plus a manifest.
const chunkSize = 8 << 20

// maxContentSize caps total content addressed by one manifest (1 GiB).
const maxContentSize = 1 << 30

// maxManifestChunks bounds a manifest's chunk list (maxContentSize/chunkSize).
const maxManifestChunks = maxContentSize / chunkSize

// ErrBlobTooLarge is returned when data exceeds maxBlobSize.
var ErrBlobTooLarge = fmt.Errorf("blob exceeds max size of %d bytes", maxBlobSize)

// ErrContentTooLarge is returned when content exceeds maxContentSize.
var ErrContentTooLarge = fmt.Errorf("content exceeds max size of %d bytes", maxContentSize)

// ErrBlobNotFound is returned when a hash is not in the local store.
var ErrBlobNotFound = errors.New("blob not found")

// BlobStore is a content-addressed store backed by a directory on disk.
type BlobStore struct {
	dir string
}

// NewBlobStore opens (creating if needed) a content store at dir.
func NewBlobStore(dir string) (*BlobStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create content dir: %w", err)
	}
	return &BlobStore{dir: dir}, nil
}

// defaultContentDir returns ~/.freedom/content.
func defaultContentDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".freedom", "content"), nil
}

// contentHash returns the base36 sha2-256 multihash address of data — the same
// encoding pubKeyID uses, so content hashes and key ids share one format.
func contentHash(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	h, err := mh.Encode(sum[:], mh.SHA2_256)
	if err != nil {
		return "", err
	}
	return base36.EncodeToStringLc(h), nil
}

// isContentHash reports whether s is a well-formed base36 sha2-256 multihash.
func isContentHash(s string) bool {
	raw, err := base36.DecodeString(s)
	if err != nil {
		return false
	}
	decoded, err := mh.Decode(raw)
	if err != nil {
		return false
	}
	return decoded.Code == mh.SHA2_256 && len(decoded.Digest) == sha256.Size
}

// path returns the on-disk path for a content hash. The hash is a safe base36
// string (no path separators), used directly as the filename.
func (s *BlobStore) path(hash string) string {
	return filepath.Join(s.dir, hash)
}

// Put stores data and returns its content hash. It is idempotent: storing the
// same bytes twice yields the same hash and overwrites identically.
func (s *BlobStore) Put(data []byte) (string, error) {
	if len(data) > maxBlobSize {
		return "", ErrBlobTooLarge
	}
	hash, err := contentHash(data)
	if err != nil {
		return "", err
	}
	if s.Has(hash) {
		return hash, nil // already stored; content-addressed => identical
	}
	// Write atomically via a temp file + rename so a crash never leaves a
	// partial blob under a valid hash.
	tmp, err := os.CreateTemp(s.dir, ".put-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, s.path(hash)); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return hash, nil
}

// Get returns the bytes for a content hash, or ErrBlobNotFound.
func (s *BlobStore) Get(hash string) ([]byte, error) {
	if !isContentHash(hash) {
		return nil, fmt.Errorf("invalid content hash %q", hash)
	}
	data, err := os.ReadFile(s.path(hash))
	if os.IsNotExist(err) {
		return nil, ErrBlobNotFound
	}
	return data, err
}

// Has reports whether the store holds a blob for the hash.
func (s *BlobStore) Has(hash string) bool {
	if !isContentHash(hash) {
		return false
	}
	_, err := os.Stat(s.path(hash))
	return err == nil
}

// Delete removes a blob. A missing blob is not an error (delete is used by
// eviction, which may race with reconciliation).
func (s *BlobStore) Delete(hash string) error {
	if !isContentHash(hash) {
		return fmt.Errorf("invalid content hash %q", hash)
	}
	err := os.Remove(s.path(hash))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Open returns a streaming reader for a hash (so serving a blob to a peer does
// not have to buffer it all in memory). Caller must Close.
func (s *BlobStore) Open(hash string) (io.ReadCloser, int64, error) {
	if !isContentHash(hash) {
		return nil, 0, fmt.Errorf("invalid content hash %q", hash)
	}
	f, err := os.Open(s.path(hash))
	if os.IsNotExist(err) {
		return nil, 0, ErrBlobNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// --- chunk manifests ---
//
// Large content is addressed by the hash of a *manifest* blob: a magic header
// plus JSON naming the chunk hashes in order. Chunks are ordinary blobs, so
// the transfer protocol and provider records need no new machinery — a reader
// fetches the manifest, then each chunk, each verified against its own hash.

// manifestMagic is the first bytes of every manifest blob. A blob is treated
// as a manifest only if it starts with this prefix AND the remainder parses as
// a strictly valid manifest (decodeManifest), so ordinary content is not
// misread as one.
const manifestMagic = "freedom-names/manifest@1\n"

// ChunkManifest describes content split into fixed-size chunks. Every chunk is
// exactly ChunkSize bytes except the last, which holds the remainder.
type ChunkManifest struct {
	TotalSize int64    `json:"totalSize"`
	ChunkSize int64    `json:"chunkSize"`
	Chunks    []string `json:"chunks"`
}

// chunkLen returns the expected byte length of chunk i.
func (m *ChunkManifest) chunkLen(i int) int64 {
	if i == len(m.Chunks)-1 {
		return m.TotalSize - int64(len(m.Chunks)-1)*m.ChunkSize
	}
	return m.ChunkSize
}

// encodeManifest serializes a manifest to its blob bytes.
func encodeManifest(m *ChunkManifest) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append([]byte(manifestMagic), body...), nil
}

// decodeManifest reports whether data is a valid manifest blob. Validation is
// strict — magic prefix, well-formed hashes, and a chunk count that exactly
// matches TotalSize — so a random blob cannot pass by accident.
func decodeManifest(data []byte) (*ChunkManifest, bool) {
	if !bytes.HasPrefix(data, []byte(manifestMagic)) {
		return nil, false
	}
	var m ChunkManifest
	if err := json.Unmarshal(data[len(manifestMagic):], &m); err != nil {
		return nil, false
	}
	n := int64(len(m.Chunks))
	if m.ChunkSize < 1 || m.ChunkSize > maxBlobSize {
		return nil, false
	}
	// Single-chunk content is stored as a plain blob, so a real manifest has
	// at least two chunks.
	if n < 2 || n > maxManifestChunks {
		return nil, false
	}
	if m.TotalSize > maxContentSize || m.TotalSize <= (n-1)*m.ChunkSize || m.TotalSize > n*m.ChunkSize {
		return nil, false
	}
	for _, h := range m.Chunks {
		if !isContentHash(h) {
			return nil, false
		}
	}
	return &m, true
}

// List returns the hashes currently stored (used by the keep-providing loop).
func (s *BlobStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && isContentHash(name) {
			hashes = append(hashes, name)
		}
	}
	return hashes, nil
}
