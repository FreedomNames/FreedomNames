package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestContentHandlerSniffsContentType verifies GET /content serves a sniffed
// Content-Type instead of a blanket application/octet-stream, so browsers can
// render images (and other media) fetched from the content network.
func TestContentHandlerSniffsContentType(t *testing.T) {
	cs := testContentService(t)
	server := httptest.NewServer(ContentHandler(cs))
	defer server.Close()

	pngHeader := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"png", append(pngHeader, testBytes(64)...), "image/png"},
		{"jpeg", append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF"), testBytes(64)...), "image/jpeg"},
		{"gif", append([]byte("GIF89a"), testBytes(64)...), "image/gif"},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), testBytes(64)...), "image/webp"},
		{"markdown", []byte("# Hello\n\nJust text.\n"), "text/plain; charset=utf-8"},
		// A chunked upload sniffs from the first chunk, not the manifest.
		{"chunked-png", append(pngHeader, testBytes(chunkSize+4096)...), "image/png"},
		// Shorter than the 512-byte sniff window must still work.
		{"tiny", []byte("hi"), "text/plain; charset=utf-8"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := uploadContent(server.URL, bytes.NewReader(tc.data))
			if err != nil {
				t.Fatalf("uploadContent: %v", err)
			}
			get, err := http.Get(server.URL + "/content?hash=" + hash)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer get.Body.Close()
			if got := get.Header.Get("Content-Type"); got != tc.want {
				t.Fatalf("Content-Type %q, want %q", got, tc.want)
			}
			if get.ContentLength != int64(len(tc.data)) {
				t.Fatalf("Content-Length %d, want %d", get.ContentLength, len(tc.data))
			}
			body, err := io.ReadAll(get.Body)
			if err != nil || !bytes.Equal(body, tc.data) {
				t.Fatalf("GET body: err=%v equal=%v", err, bytes.Equal(body, tc.data))
			}
		})
	}
}
