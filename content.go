package main

import (
	"crypto/sha256"
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

// maxBlobSize caps a single blob (32 MiB). Whole-page content plus reasonable
// assets fit comfortably; chunking for larger media is a later phase.
const maxBlobSize = 32 << 20

// ErrBlobTooLarge is returned when data exceeds maxBlobSize.
var ErrBlobTooLarge = fmt.Errorf("blob exceeds max size of %d bytes", maxBlobSize)

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
