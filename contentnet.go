package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-base36"
	mh "github.com/multiformats/go-multihash"
)

// ContentService moves page bytes between peers. The DHT is used only to
// discover *which* peers hold a hash (provider records); the bytes themselves
// travel over a dedicated libp2p stream protocol so they never hit the
// size-limited DHT value store.
//
// Flow:
//   - Put(data): store locally, then dht.Provide(cid) so others can find us.
//   - Fetch(hash): return local bytes, or FindProvidersAsync -> dial a provider
//     -> stream the blob -> verify the hash -> cache locally.
type ContentService struct {
	store *BlobStore
	node  *FreedomNameNode
}

// contentProtocol is the libp2p stream protocol id for blob transfer.
const contentProtocol = protocol.ID("/freedomnames/content/1.0.0")

// Wire-format limits for a request.
const maxHashRequestLen = 128

// provideInterval is how often owned content is re-provided so the provider
// records do not expire (DHT provider records last ~24h/48h depending on config).
const provideInterval = 12 * time.Hour

// NewContentService creates the service and registers its stream handler on the
// node's libp2p host.
func NewContentService(node *FreedomNameNode, store *BlobStore) *ContentService {
	cs := &ContentService{store: store, node: node}
	node.kadDHT.Host().SetStreamHandler(contentProtocol, cs.handleStream)
	go cs.provideLoop()
	return cs
}

// hashToCID wraps a base36 sha2-256 multihash content hash in a raw CIDv1, the
// key the DHT provider index uses.
func hashToCID(hash string) (cid.Cid, error) {
	raw, err := base36.DecodeString(hash)
	if err != nil {
		return cid.Undef, fmt.Errorf("bad content hash: %w", err)
	}
	if _, err := mh.Decode(raw); err != nil {
		return cid.Undef, fmt.Errorf("bad multihash: %w", err)
	}
	return cid.NewCidV1(cid.Raw, raw), nil
}

// Put stores data locally and announces it to the DHT so peers can fetch it.
func (cs *ContentService) Put(ctx context.Context, data []byte) (string, error) {
	hash, err := cs.store.Put(data)
	if err != nil {
		return "", err
	}
	cs.announce(ctx, hash)
	return hash, nil
}

// announce publishes a provider record for a hash (best-effort; a failure to
// announce does not fail the Put, since the content is stored locally).
func (cs *ContentService) announce(ctx context.Context, hash string) {
	c, err := hashToCID(hash)
	if err != nil {
		return
	}
	provideCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := cs.node.kadDHT.Provide(provideCtx, c, true); err != nil {
		log.Printf("content: provide %s: %v", hash, err)
	}
}

// Fetch returns the bytes for a content hash: from the local store, or by
// discovering a provider via the DHT and streaming the blob from it. A fetched
// blob is verified against its hash and cached locally.
func (cs *ContentService) Fetch(ctx context.Context, hash string) ([]byte, error) {
	if data, err := cs.store.Get(hash); err == nil {
		return data, nil
	}

	c, err := hashToCID(hash)
	if err != nil {
		return nil, err
	}

	// Find providers and try each until one serves the blob.
	provCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	providers := cs.node.kadDHT.FindProvidersAsync(provCtx, c, 10)

	var lastErr error = ErrBlobNotFound
	for p := range providers {
		if p.ID == cs.node.kadDHT.Host().ID() {
			continue
		}
		data, err := cs.fetchFrom(ctx, p, hash)
		if err != nil {
			lastErr = err
			continue
		}
		// Store the fetched (already hash-verified) blob for future requests.
		if _, err := cs.store.Put(data); err != nil {
			log.Printf("content: cache %s: %v", hash, err)
		}
		return data, nil
	}
	return nil, lastErr
}

// fetchFrom opens a content stream to a peer, requests a hash, reads the blob,
// and verifies it matches the requested hash.
func (cs *ContentService) fetchFrom(ctx context.Context, p peer.AddrInfo, hash string) ([]byte, error) {
	cs.node.kadDHT.Host().Peerstore().AddAddrs(p.ID, p.Addrs, time.Hour)

	streamCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stream, err := cs.node.kadDHT.Host().NewStream(streamCtx, p.ID, contentProtocol)
	if err != nil {
		return nil, fmt.Errorf("open stream to %s: %w", p.ID, err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(20 * time.Second))

	if err := writeRequest(stream, hash); err != nil {
		return nil, err
	}
	data, err := readBlob(stream)
	if err != nil {
		return nil, err
	}

	// Verify the peer served the bytes we asked for.
	got, err := contentHash(data)
	if err != nil {
		return nil, err
	}
	if got != hash {
		return nil, fmt.Errorf("provider served wrong content (asked %s, got %s)", hash, got)
	}
	return data, nil
}

// handleStream serves an inbound content request: read a hash, write the blob
// (length-prefixed) if we hold it, else write a zero length.
func (cs *ContentService) handleStream(stream network.Stream) {
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(20 * time.Second))

	hash, err := readRequest(stream)
	if err != nil {
		return
	}
	rc, size, err := cs.store.Open(hash)
	if err != nil {
		writeBlobHeader(stream, 0) // not found: zero-length blob
		return
	}
	defer rc.Close()
	if err := writeBlobHeader(stream, uint64(size)); err != nil {
		return
	}
	io.Copy(stream, rc)
}

// provideLoop re-announces every locally stored blob periodically so provider
// records stay fresh while this node is up.
func (cs *ContentService) provideLoop() {
	// Announce shortly after start, then on an interval.
	first := time.NewTimer(20 * time.Second)
	defer first.Stop()
	for {
		select {
		case <-first.C:
			cs.provideAll()
		case <-time.After(provideInterval):
			cs.provideAll()
		case <-cs.node.ctx.Done():
			return
		}
	}
}

func (cs *ContentService) provideAll() {
	hashes, err := cs.store.List()
	if err != nil {
		log.Printf("content: list for provide: %v", err)
		return
	}
	for _, h := range hashes {
		select {
		case <-cs.node.ctx.Done():
			return
		default:
		}
		cs.announce(cs.node.ctx, h)
	}
}

// --- wire format: request = varint-len + hash string; blob = varint-len + bytes ---

func writeRequest(w io.Writer, hash string) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], uint64(len(hash)))
	if _, err := w.Write(buf[:n]); err != nil {
		return err
	}
	_, err := io.WriteString(w, hash)
	return err
}

func readRequest(r io.Reader) (string, error) {
	br := newByteReaderFrom(r)
	n, err := binary.ReadUvarint(br)
	if err != nil {
		return "", err
	}
	if n == 0 || n > maxHashRequestLen {
		return "", errors.New("bad content request length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func writeBlobHeader(w io.Writer, size uint64) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], size)
	_, err := w.Write(buf[:n])
	return err
}

func readBlob(r io.Reader) ([]byte, error) {
	size, err := binary.ReadUvarint(newByteReaderFrom(r))
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, ErrBlobNotFound
	}
	if size > maxBlobSize {
		return nil, ErrBlobTooLarge
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// byteReaderFrom adapts an io.Reader to io.ByteReader for binary.ReadUvarint.
// It reads exactly one byte per call, so it never over-reads past the varint
// and the caller can continue reading the payload directly from r.
type byteReaderFrom struct {
	r   io.Reader
	one [1]byte
}

func newByteReaderFrom(r io.Reader) *byteReaderFrom { return &byteReaderFrom{r: r} }

func (b *byteReaderFrom) ReadByte() (byte, error) {
	if _, err := io.ReadFull(b.r, b.one[:]); err != nil {
		return 0, err
	}
	return b.one[0], nil
}
