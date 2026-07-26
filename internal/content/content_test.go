package content

import (
	"bytes"
	"io"
	"testing"
)

func newTestStore(t *testing.T) *BlobStore {
	t.Helper()
	s, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func TestBlobStoreRoundTrip(t *testing.T) {
	s := newTestStore(t)
	data := []byte("# Hello Freedom Web\n\nThis is a page.")

	hash, err := s.Put(data)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !IsContentHash(hash) {
		t.Fatalf("returned hash %q is not a valid content hash", hash)
	}
	if !s.Has(hash) {
		t.Fatal("Has returned false after Put")
	}

	got, err := s.Get(hash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("Get returned different bytes")
	}
}

func TestBlobStoreIdempotent(t *testing.T) {
	s := newTestStore(t)
	data := []byte("same bytes")
	h1, _ := s.Put(data)
	h2, _ := s.Put(data)
	if h1 != h2 {
		t.Fatalf("same content produced different hashes: %s vs %s", h1, h2)
	}
}

func TestBlobStoreDeterministicHash(t *testing.T) {
	// The hash is a pure function of the bytes, independent of the store.
	a, _ := ContentHash([]byte("abc"))
	b, _ := ContentHash([]byte("abc"))
	c, _ := ContentHash([]byte("abd"))
	if a != b {
		t.Fatal("hash not deterministic")
	}
	if a == c {
		t.Fatal("different content produced the same hash")
	}
	if !IsContentHash(a) {
		t.Fatalf("ContentHash produced an invalid hash: %s", a)
	}
}

func TestBlobStoreMissing(t *testing.T) {
	s := newTestStore(t)
	missing, _ := ContentHash([]byte("never stored"))
	if s.Has(missing) {
		t.Fatal("Has true for missing blob")
	}
	if _, err := s.Get(missing); err != ErrBlobNotFound {
		t.Fatalf("expected ErrBlobNotFound, got %v", err)
	}
}

func TestBlobStoreCap(t *testing.T) {
	s := newTestStore(t)
	big := make([]byte, MaxBlobSize+1)
	if _, err := s.Put(big); err != ErrBlobTooLarge {
		t.Fatalf("expected ErrBlobTooLarge, got %v", err)
	}
}

func TestBlobStoreStreamAndList(t *testing.T) {
	s := newTestStore(t)
	data := []byte("streamed content")
	hash, _ := s.Put(data)

	rc, size, err := s.Open(hash)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	if size != int64(len(data)) {
		t.Fatalf("size %d != %d", size, len(data))
	}
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatal("streamed bytes differ")
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0] != hash {
		t.Fatalf("unexpected list: %v", list)
	}
}

func TestInvalidContentHashRejected(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "not-a-hash", "../escape", "UPPERCASE"} {
		if s.Has(bad) {
			t.Errorf("Has true for invalid hash %q", bad)
		}
		if _, err := s.Get(bad); err == nil {
			t.Errorf("Get succeeded for invalid hash %q", bad)
		}
	}
}
