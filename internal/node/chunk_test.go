package node

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"gitlab.melroy.org/freedom-names/freedom-names/internal/content"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/testsupport"
)

// testContentService returns a store-only ContentService (no node/DHT), which
// exercises the local chunking and reassembly paths.
func testContentService(t *testing.T) *ContentService {
	t.Helper()
	store, err := content.NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return NewLocalContentService(store)
}

func TestPutStreamSingleBlob(t *testing.T) {
	cs := testContentService(t)
	ctx := context.Background()

	for _, size := range []int{100, content.ChunkSize} {
		data := testsupport.TestBytes(size)
		hash, n, err := cs.PutStream(ctx, bytes.NewReader(data))
		if err != nil {
			t.Fatalf("PutStream(%d): %v", size, err)
		}
		if n != int64(size) {
			t.Fatalf("PutStream(%d) reported %d bytes", size, n)
		}
		want, _ := content.ContentHash(data)
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

	data := testsupport.TestBytes(2*content.ChunkSize + 12345) // 3 chunks, partial last
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
	m, ok := content.DecodeManifest(blob)
	if !ok {
		t.Fatalf("stored blob is not a valid manifest")
	}
	if len(m.Chunks) != 3 || m.TotalSize != int64(len(data)) || m.ChunkSize != content.ChunkSize {
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

// TestPutStreamChunkBoundary checks content of exactly N*content.ChunkSize produces N
// full chunks (no empty trailing chunk).

func TestPutStreamChunkBoundary(t *testing.T) {
	cs := testContentService(t)
	data := testsupport.TestBytes(2 * content.ChunkSize)
	hash, _, err := cs.PutStream(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	blob, _ := cs.store.Get(hash)
	m, ok := content.DecodeManifest(blob)
	if !ok {
		t.Fatalf("expected a manifest at the boundary size")
	}
	if len(m.Chunks) != 2 || m.ChunkLen(1) != content.ChunkSize {
		t.Fatalf("boundary layout: %d chunks, last %d bytes", len(m.Chunks), m.ChunkLen(1))
	}
	got, err := cs.Fetch(context.Background(), hash)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("fetch back: err=%v equal=%v", err, bytes.Equal(got, data))
	}
}

// TestChunkReaderRejectsWrongLength checks a chunk whose length disagrees with
// the manifest fails the read instead of silently corrupting the output.

func TestChunkReaderRejectsWrongLength(t *testing.T) {
	h1, _ := content.ContentHash([]byte("chunk one"))
	h2, _ := content.ContentHash([]byte("chunk two"))
	m := &content.ChunkManifest{TotalSize: 10, ChunkSize: 8, Chunks: []string{h1, h2}}

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
	h1, _ := content.ContentHash([]byte("chunk one"))
	h2, _ := content.ContentHash([]byte("chunk two"))
	m := &content.ChunkManifest{TotalSize: 10, ChunkSize: 8, Chunks: []string{h1, h2}}

	cr := &chunkReader{manifest: m, fetch: func(hash string) ([]byte, error) {
		return nil, fmt.Errorf("no providers")
	}}
	if _, err := io.ReadAll(cr); err == nil {
		t.Fatalf("expected fetch error to propagate")
	}
}

// TestContentHandlerChunkedRoundTrip pushes multi-chunk content through the
// real HTTP handlers: POST /content, then GET /content?hash= streams it back.
