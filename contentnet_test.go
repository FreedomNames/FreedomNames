package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestContentWireFraming checks the length-prefixed request/blob framing round
// trips through an in-memory pipe.
func TestContentWireFraming(t *testing.T) {
	// Request framing.
	var reqBuf bytes.Buffer
	if err := writeRequest(&reqBuf, "myhash123"); err != nil {
		t.Fatalf("writeRequest: %v", err)
	}
	got, err := readRequest(&reqBuf)
	if err != nil {
		t.Fatalf("readRequest: %v", err)
	}
	if got != "myhash123" {
		t.Fatalf("request round-trip: got %q", got)
	}

	// Blob framing.
	blob := []byte("some page bytes")
	var blobBuf bytes.Buffer
	writeBlobHeader(&blobBuf, uint64(len(blob)))
	blobBuf.Write(blob)
	readBack, err := readBlob(&blobBuf)
	if err != nil {
		t.Fatalf("readBlob: %v", err)
	}
	if !bytes.Equal(readBack, blob) {
		t.Fatalf("blob round-trip mismatch")
	}

	// Zero-length blob signals not-found.
	var empty bytes.Buffer
	writeBlobHeader(&empty, 0)
	if _, err := readBlob(&empty); err != ErrBlobNotFound {
		t.Fatalf("expected ErrBlobNotFound for zero blob, got %v", err)
	}
}

// TestContentStreamTransfer stands up two real libp2p hosts and transfers a blob
// over the content stream protocol, proving the handler + fetch path work over
// the wire (independent of the DHT discovery layer).
func TestContentStreamTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server := newTestHost(t)
	client := newTestHost(t)
	defer server.Close()
	defer client.Close()

	// The server serves blobs from its store via the content protocol.
	store, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	data := []byte("# A page served peer-to-peer")
	hash, _ := store.Put(data)

	server.SetStreamHandler(contentProtocol, func(stream network.Stream) {
		defer stream.Close()
		reqHash, err := readRequest(stream)
		if err != nil {
			return
		}
		rc, size, err := store.Open(reqHash)
		if err != nil {
			writeBlobHeader(stream, 0)
			return
		}
		defer rc.Close()
		writeBlobHeader(stream, uint64(size))
		buf := make([]byte, size)
		rc.Read(buf)
		stream.Write(buf)
	})

	// Connect client -> server.
	if err := client.Connect(ctx, peer.AddrInfo{ID: server.ID(), Addrs: server.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Client requests the blob.
	stream, err := client.NewStream(ctx, server.ID(), contentProtocol)
	if err != nil {
		t.Fatalf("new stream: %v", err)
	}
	defer stream.Close()
	if err := writeRequest(stream, hash); err != nil {
		t.Fatalf("write request: %v", err)
	}
	got, err := readBlob(stream)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("transferred blob mismatch")
	}
	// Verify the received bytes hash to what we asked for.
	if h, _ := contentHash(got); h != hash {
		t.Fatalf("received content hash %s != requested %s", h, hash)
	}
}

func newTestHost(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	return h
}
