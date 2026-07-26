package content

import (
	"math/rand"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	h1, _ := ContentHash([]byte("chunk one"))
	h2, _ := ContentHash([]byte("chunk two"))
	m := &ChunkManifest{TotalSize: ChunkSize + 100, ChunkSize: ChunkSize, Chunks: []string{h1, h2}}

	data, err := EncodeManifest(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, ok := DecodeManifest(data)
	if !ok {
		t.Fatalf("decode rejected a valid manifest")
	}
	if got.TotalSize != m.TotalSize || got.ChunkSize != m.ChunkSize || len(got.Chunks) != 2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if got.ChunkLen(0) != ChunkSize || got.ChunkLen(1) != 100 {
		t.Fatalf("ChunkLen: got %d, %d", got.ChunkLen(0), got.ChunkLen(1))
	}
}

func TestDecodeManifestRejects(t *testing.T) {
	h1, _ := ContentHash([]byte("a"))
	h2, _ := ContentHash([]byte("b"))
	valid := func() *ChunkManifest {
		return &ChunkManifest{TotalSize: ChunkSize + 1, ChunkSize: ChunkSize, Chunks: []string{h1, h2}}
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
			d, _ := EncodeManifest(m)
			return d
		}},
		{"invalid chunk hash", func() []byte {
			m := valid()
			m.Chunks[1] = "not-a-hash"
			d, _ := EncodeManifest(m)
			return d
		}},
		{"total too small for chunk count", func() []byte {
			m := valid()
			m.TotalSize = ChunkSize // would fit in one chunk
			d, _ := EncodeManifest(m)
			return d
		}},
		{"total too large for chunk count", func() []byte {
			m := valid()
			m.TotalSize = 2*ChunkSize + 1
			d, _ := EncodeManifest(m)
			return d
		}},
		{"zero chunk size", func() []byte {
			m := valid()
			m.ChunkSize = 0
			d, _ := EncodeManifest(m)
			return d
		}},
		{"content over max size", func() []byte {
			m := valid()
			m.ChunkSize = MaxBlobSize
			m.Chunks = make([]string, MaxManifestChunks)
			for i := range m.Chunks {
				m.Chunks[i] = h1
			}
			m.TotalSize = int64(len(m.Chunks)) * m.ChunkSize // 4 GiB > MaxContentSize
			d, _ := EncodeManifest(m)
			return d
		}},
	}
	for _, tc := range cases {
		if _, ok := DecodeManifest(tc.data()); ok {
			t.Errorf("%s: DecodeManifest accepted invalid input", tc.name)
		}
	}
}

// TestPutStreamSingleBlob checks content up to one chunk keeps the plain
// content-hash address (no manifest), including exactly at the boundary.

// testBytes returns n deterministic pseudo-random bytes. Kept local rather than
// taken from internal/testsupport: that package imports internal/record, which
// imports this one, and a test binary may not close an import cycle either.
func testBytes(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(42)).Read(data)
	return data
}
