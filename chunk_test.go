package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testContentService returns a store-only ContentService (no node/DHT), which
// exercises the local chunking and reassembly paths.
func testContentService(t *testing.T) *ContentService {
	t.Helper()
	store, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &ContentService{store: store}
}

// testBytes returns n deterministic pseudo-random bytes.
func testBytes(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(42)).Read(data)
	return data
}

func TestManifestRoundTrip(t *testing.T) {
	h1, _ := contentHash([]byte("chunk one"))
	h2, _ := contentHash([]byte("chunk two"))
	m := &ChunkManifest{TotalSize: chunkSize + 100, ChunkSize: chunkSize, Chunks: []string{h1, h2}}

	data, err := encodeManifest(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok := decodeManifest(data)
	if !ok {
		t.Fatalf("decode rejected a valid manifest")
	}
	if got.TotalSize != m.TotalSize || got.ChunkSize != m.ChunkSize || len(got.Chunks) != 2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.chunkLen(0) != chunkSize || got.chunkLen(1) != 100 {
		t.Fatalf("chunkLen: got %d, %d", got.chunkLen(0), got.chunkLen(1))
	}
}

func TestDecodeManifestRejects(t *testing.T) {
	h1, _ := contentHash([]byte("a"))
	h2, _ := contentHash([]byte("b"))
	valid := func() *ChunkManifest {
		return &ChunkManifest{TotalSize: chunkSize + 1, ChunkSize: chunkSize, Chunks: []string{h1, h2}}
	}

	cases := []struct {
		name string
		data func() []byte
	}{
		{"not a manifest", func() []byte { return []byte("just a page") }},
		{"magic but bad json", func() []byte { return []byte(manifestMagic + "{oops") }},
		{"single chunk", func() []byte {
			m := valid()
			m.Chunks = m.Chunks[:1]
			m.TotalSize = 5
			d, _ := encodeManifest(m)
			return d
		}},
		{"invalid chunk hash", func() []byte {
			m := valid()
			m.Chunks[1] = "not-a-hash"
			d, _ := encodeManifest(m)
			return d
		}},
		{"total too small for chunk count", func() []byte {
			m := valid()
			m.TotalSize = chunkSize // would fit in one chunk
			d, _ := encodeManifest(m)
			return d
		}},
		{"total too large for chunk count", func() []byte {
			m := valid()
			m.TotalSize = 2*chunkSize + 1
			d, _ := encodeManifest(m)
			return d
		}},
		{"zero chunk size", func() []byte {
			m := valid()
			m.ChunkSize = 0
			d, _ := encodeManifest(m)
			return d
		}},
		{"content over max size", func() []byte {
			m := valid()
			m.ChunkSize = maxBlobSize
			m.Chunks = make([]string, maxManifestChunks)
			for i := range m.Chunks {
				m.Chunks[i] = h1
			}
			m.TotalSize = int64(len(m.Chunks)) * m.ChunkSize // 4 GiB > maxContentSize
			d, _ := encodeManifest(m)
			return d
		}},
	}
	for _, tc := range cases {
		if _, ok := decodeManifest(tc.data()); ok {
			t.Errorf("%s: decodeManifest accepted invalid input", tc.name)
		}
	}
}

// TestPutStreamSingleBlob checks content up to one chunk keeps the plain
// content-hash address (no manifest), including exactly at the boundary.
func TestPutStreamSingleBlob(t *testing.T) {
	cs := testContentService(t)
	ctx := context.Background()

	for _, size := range []int{100, chunkSize} {
		data := testBytes(size)
		hash, n, err := cs.PutStream(ctx, bytes.NewReader(data))
		if err != nil {
			t.Fatalf("PutStream(%d): %v", size, err)
		}
		if n != int64(size) {
			t.Fatalf("PutStream(%d) reported %d bytes", size, n)
		}
		want, _ := contentHash(data)
		if hash != want {
			t.Fatalf("size %d stored as %s, want plain content hash %s", size, hash, want)
		}
		got, err := cs.Fetch(ctx, hash)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("fetch back size %d: err=%v equal=%v", size, err, bytes.Equal(got, data))
		}
	}
}

// TestPutStreamChunked stores content spanning multiple chunks and verifies
// the manifest layout and full reassembly via both Fetch and FetchStream.
func TestPutStreamChunked(t *testing.T) {
	cs := testContentService(t)
	ctx := context.Background()

	data := testBytes(2*chunkSize + 12345) // 3 chunks, partial last
	hash, n, err := cs.PutStream(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("reported %d bytes, want %d", n, len(data))
	}

	// The address is the manifest's hash, and the manifest lists 3 stored chunks.
	blob, err := cs.store.Get(hash)
	if err != nil {
		t.Fatalf("manifest blob: %v", err)
	}
	m, ok := decodeManifest(blob)
	if !ok {
		t.Fatalf("stored blob is not a valid manifest")
	}
	if len(m.Chunks) != 3 || m.TotalSize != int64(len(data)) || m.ChunkSize != chunkSize {
		t.Fatalf("manifest layout: %d chunks, total %d", len(m.Chunks), m.TotalSize)
	}
	for i, ch := range m.Chunks {
		if !cs.store.Has(ch) {
			t.Fatalf("chunk %d not in store", i)
		}
	}

	// Reassembly.
	got, err := cs.Fetch(ctx, hash)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("reassembled bytes differ")
	}
	rc, size, err := cs.FetchStream(ctx, hash)
	if err != nil {
		t.Fatalf("FetchStream: %v", err)
	}
	defer rc.Close()
	if size != int64(len(data)) {
		t.Fatalf("FetchStream size %d, want %d", size, len(data))
	}
	streamed, err := io.ReadAll(rc)
	if err != nil || !bytes.Equal(streamed, data) {
		t.Fatalf("streamed reassembly: err=%v equal=%v", err, bytes.Equal(streamed, data))
	}
}

// TestPutStreamChunkBoundary checks content of exactly N*chunkSize produces N
// full chunks (no empty trailing chunk).
func TestPutStreamChunkBoundary(t *testing.T) {
	cs := testContentService(t)
	data := testBytes(2 * chunkSize)
	hash, _, err := cs.PutStream(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	blob, _ := cs.store.Get(hash)
	m, ok := decodeManifest(blob)
	if !ok {
		t.Fatalf("expected a manifest at the boundary size")
	}
	if len(m.Chunks) != 2 || m.chunkLen(1) != chunkSize {
		t.Fatalf("boundary layout: %d chunks, last %d bytes", len(m.Chunks), m.chunkLen(1))
	}
	got, err := cs.Fetch(context.Background(), hash)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("fetch back: err=%v equal=%v", err, bytes.Equal(got, data))
	}
}

// TestChunkReaderRejectsWrongLength checks a chunk whose length disagrees with
// the manifest fails the read instead of silently corrupting the output.
func TestChunkReaderRejectsWrongLength(t *testing.T) {
	h1, _ := contentHash([]byte("chunk one"))
	h2, _ := contentHash([]byte("chunk two"))
	m := &ChunkManifest{TotalSize: 10, ChunkSize: 8, Chunks: []string{h1, h2}}

	cr := &chunkReader{manifest: m, fetch: func(hash string) ([]byte, error) {
		return []byte("wrong-size-chunk"), nil // 16 bytes, manifest says 8
	}}
	if _, err := io.ReadAll(cr); err == nil {
		t.Fatalf("expected length-mismatch error")
	}
}

// TestChunkReaderPropagatesFetchError checks an unfetchable chunk surfaces as
// a read error naming the chunk.
func TestChunkReaderPropagatesFetchError(t *testing.T) {
	h1, _ := contentHash([]byte("chunk one"))
	h2, _ := contentHash([]byte("chunk two"))
	m := &ChunkManifest{TotalSize: 10, ChunkSize: 8, Chunks: []string{h1, h2}}

	cr := &chunkReader{manifest: m, fetch: func(hash string) ([]byte, error) {
		return nil, fmt.Errorf("no providers")
	}}
	if _, err := io.ReadAll(cr); err == nil {
		t.Fatalf("expected fetch error to propagate")
	}
}

// TestContentHandlerChunkedRoundTrip pushes multi-chunk content through the
// real HTTP handlers: POST /content, then GET /content?hash= streams it back.
func TestContentHandlerChunkedRoundTrip(t *testing.T) {
	cs := testContentService(t)
	server := httptest.NewServer(ContentHandler(cs))
	defer server.Close()

	data := testBytes(chunkSize + 4096)
	// uploadContent is the CLI's streaming upload path (POSTs to /content).
	hash, err := uploadContent(server.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("uploadContent: %v", err)
	}

	get, err := http.Get(server.URL + "/content?hash=" + hash)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d", get.StatusCode)
	}
	if got := get.ContentLength; got != int64(len(data)) {
		t.Fatalf("Content-Length %d, want %d", got, len(data))
	}
	body, err := io.ReadAll(get.Body)
	if err != nil || !bytes.Equal(body, data) {
		t.Fatalf("GET body: err=%v equal=%v", err, bytes.Equal(body, data))
	}
}
