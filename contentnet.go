package main

import (
	"bytes"
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
	"golang.org/x/time/rate"
)

// ContentService moves page bytes between peers. The DHT is used only to
// discover *which* peers hold a hash (provider records); the bytes themselves
// travel over a dedicated libp2p stream protocol so they never hit the
// size-limited DHT value store.
//
// Flow:
//   - PutStream(r): store locally (chunking content larger than chunkSize into
//     chunk blobs plus a manifest), then dht.Provide(cid) so others can find us.
//   - FetchStream(hash): return local bytes, or FindProvidersAsync -> dial a
//     provider -> stream the blob -> verify the hash -> cache locally. If the
//     blob is a manifest, chunks are fetched the same way, one at a time.
type ContentService struct {
	store *BlobStore
	node  *FreedomNameNode

	// Replication + hosting policy (see replicate.go / contentindex.go). All
	// nil/zero in store-only test construction: index nil-guards, limiters
	// pass through, and rep is only used when a node is attached.
	index        *ContentIndex
	rep          *replicator
	hostBudget   int64
	maxPushSize  int64
	hostTTL      time.Duration
	healInterval time.Duration
	upLimit      *rate.Limiter
	downLimit    *rate.Limiter
}

// contentProtocol is the libp2p stream protocol id for blob transfer.
const contentProtocol = protocol.ID("/freedomnames/content/1.0.0")

// Wire-format limits for a request.
const maxHashRequestLen = 128

// provideInterval is how often owned content is re-provided so the provider
// records do not expire (DHT provider records last ~24h/48h depending on config).
const provideInterval = 12 * time.Hour

// NewContentService creates the service, registers the fetch and push stream
// handlers on the node's libp2p host, and starts the keep-providing and
// replica-healing loops.
func NewContentService(node *FreedomNameNode, store *BlobStore, cfg *Config) *ContentService {
	cs := &ContentService{
		store:        store,
		node:         node,
		hostBudget:   cfg.ContentHostBudget,
		maxPushSize:  cfg.ContentMaxPushSize,
		hostTTL:      cfg.ContentHostTTL,
		healInterval: cfg.ContentHealInterval,
		upLimit:      newRateLimiter(cfg.ContentUpRate),
		downLimit:    newRateLimiter(cfg.ContentDownRate),
	}
	ix, err := LoadContentIndex(store.dir, store)
	if err != nil {
		log.Printf("WARNING: content index unavailable (hosting policy and healing disabled): %v", err)
	} else {
		cs.index = ix
	}
	cs.rep = cs.newReplicator(cfg.ContentReplicas)
	node.kadDHT.Host().SetStreamHandler(contentProtocol, cs.handleStream)
	node.kadDHT.Host().SetStreamHandler(pushProtocol, cs.handlePushStream)
	go cs.provideLoop()
	if cs.index != nil && cs.healInterval > 0 {
		go cs.healLoop()
	}
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
	hash, _, err := cs.PutStream(ctx, bytes.NewReader(data))
	return hash, err
}

// PutStream stores content of any size up to maxContentSize, reading r to the
// end. Content that fits in one chunk is stored as a single blob whose hash is
// the plain content hash (identical to pre-chunking addresses); larger content
// becomes chunk blobs plus a manifest, and the manifest's hash is returned.
// Memory use stays bounded at one chunk regardless of content size.
func (cs *ContentService) PutStream(ctx context.Context, r io.Reader) (string, int64, error) {
	// Read one byte past chunkSize to learn whether this is single- or
	// multi-chunk content before committing to either layout.
	head := make([]byte, chunkSize+1)
	hn, err := readFill(r, head)
	if err != nil {
		return "", 0, err
	}
	if hn <= chunkSize {
		hash, err := cs.store.Put(head[:hn])
		if err != nil {
			return "", 0, err
		}
		cs.index.MarkOwned(hash, int64(hn), nil)
		cs.announce(ctx, hash)
		cs.replicateOwned(hash)
		return hash, int64(hn), nil
	}

	m := ChunkManifest{ChunkSize: chunkSize}
	putChunk := func(b []byte) error {
		if m.TotalSize+int64(len(b)) > maxContentSize {
			return ErrContentTooLarge
		}
		h, err := cs.store.Put(b)
		if err != nil {
			return err
		}
		m.Chunks = append(m.Chunks, h)
		m.TotalSize += int64(len(b))
		return nil
	}
	if err := putChunk(head[:chunkSize]); err != nil {
		return "", 0, err
	}
	buf := make([]byte, chunkSize)
	off := copy(buf, head[chunkSize:hn]) // the one look-ahead byte
	for {
		n, err := readFill(r, buf[off:])
		if err != nil {
			return "", 0, err
		}
		fill := off + n
		if fill == 0 {
			break
		}
		if err := putChunk(buf[:fill]); err != nil {
			return "", 0, err
		}
		off = 0
		if fill < chunkSize {
			break
		}
	}

	data, err := encodeManifest(&m)
	if err != nil {
		return "", 0, err
	}
	hash, err := cs.store.Put(data)
	if err != nil {
		return "", 0, err
	}
	cs.index.MarkOwned(hash, int64(len(data))+m.TotalSize, m.Chunks)
	cs.announce(ctx, hash)
	// Providing every chunk means one DHT walk each; do it in the background
	// so a large Put returns promptly. provideLoop re-announces on schedule.
	go cs.announceChunks(m.Chunks)
	cs.replicateOwned(hash)
	return hash, m.TotalSize, nil
}

// replicateOwned pushes freshly published content toward its replica peers in
// the background, so publish latency is unaffected and the content no longer
// depends on this node from the very start.
func (cs *ContentService) replicateOwned(root string) {
	if cs.rep == nil {
		return
	}
	go func() {
		placed := cs.rep.replicate(cs.node.ctx, root)
		log.Printf("content: %s replicated to %d peer(s)", root, placed)
	}()
}

// readFill reads into buf until it is full or the stream ends, returning the
// count read. Unlike io.ReadFull, a clean early EOF is not an error.
func readFill(r io.Reader, buf []byte) (int, error) {
	n, err := io.ReadFull(r, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return n, nil
	}
	return n, err
}

// announce publishes a provider record for a hash (best-effort; a failure to
// announce does not fail the Put, since the content is stored locally).
func (cs *ContentService) announce(ctx context.Context, hash string) {
	if cs.node == nil {
		return // store-only service (tests): nothing to announce to
	}
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

// Fetch returns the whole bytes for a content hash, reassembling chunked
// content in memory. For large content prefer FetchStream, which holds only
// one chunk at a time.
func (cs *ContentService) Fetch(ctx context.Context, hash string) ([]byte, error) {
	rc, size, err := cs.FetchStream(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := bytes.NewBuffer(make([]byte, 0, size))
	if _, err := io.Copy(buf, rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FetchStream returns a reader over the content behind hash, plus its total
// size. A plain blob is served whole; a manifest is expanded chunk by chunk on
// demand, preferring the peer that served the manifest (it very likely holds
// the chunks too) before falling back to per-chunk provider discovery.
//
// Remotely fetched content is cached and indexed as a hosted set — becoming
// one more replica the network can rely on — but only when the operator's
// hosting policy admits it; otherwise the bytes are served without caching.
func (cs *ContentService) FetchStream(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	top, src, err := cs.fetchBlob(ctx, hash)
	if err != nil {
		return nil, 0, err
	}
	remote := src != ""
	if m, ok := decodeManifest(top); ok {
		cache := false
		if remote {
			total := int64(len(top)) + m.TotalSize
			if cs.admitHosted(total) {
				cache = true
				cs.cacheBlob(hash, top)
				cs.index.AddHosted(hash, total, m.Chunks, src.String())
			}
		} else {
			cs.index.TouchBlob(hash)
		}
		fetch := func(chunkHash string) ([]byte, error) { return cs.fetchChunk(ctx, chunkHash, src, cache) }
		return &chunkReader{manifest: m, fetch: fetch}, m.TotalSize, nil
	}
	if remote {
		if cs.admitHosted(int64(len(top))) {
			cs.cacheBlob(hash, top)
			cs.index.AddHosted(hash, int64(len(top)), nil, src.String())
		}
	} else {
		cs.index.TouchBlob(hash)
	}
	return io.NopCloser(bytes.NewReader(top)), int64(len(top)), nil
}

// fetchBlob returns one blob: from the local store, or by discovering a
// provider via the DHT and streaming it (hash-verified by fetchFrom). The
// serving peer's ID is returned so callers can request related blobs (chunks)
// from it directly and decide whether to cache the set.
func (cs *ContentService) fetchBlob(ctx context.Context, hash string) ([]byte, peer.ID, error) {
	if data, err := cs.store.Get(hash); err == nil {
		return data, "", nil
	}
	if cs.node == nil {
		return nil, "", ErrBlobNotFound // store-only service (tests)
	}

	c, err := hashToCID(hash)
	if err != nil {
		return nil, "", err
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
		return data, p.ID, nil
	}
	return nil, "", lastErr
}

// fetchChunk returns one chunk of a manifest: from the local store, from the
// peer that served the manifest, or via full provider discovery last. cache
// says whether the set was admitted for local hosting.
func (cs *ContentService) fetchChunk(ctx context.Context, hash string, src peer.ID, cache bool) ([]byte, error) {
	if data, err := cs.store.Get(hash); err == nil {
		return data, nil
	}
	if src != "" {
		if data, err := cs.fetchFrom(ctx, peer.AddrInfo{ID: src}, hash); err == nil {
			if cache {
				cs.cacheBlob(hash, data)
			}
			return data, nil
		}
	}
	data, _, err := cs.fetchBlob(ctx, hash)
	if err == nil && cache {
		cs.cacheBlob(hash, data)
	}
	return data, err
}

// cacheBlob stores a fetched (already hash-verified) blob and announces this
// node as a provider for it, so content gains replicas as it spreads.
func (cs *ContentService) cacheBlob(hash string, data []byte) {
	if _, err := cs.store.Put(data); err != nil {
		log.Printf("content: cache %s: %v", hash, err)
		return
	}
	if cs.node != nil {
		go cs.announce(cs.node.ctx, hash)
	}
}

// announceChunks best-effort provides each chunk of a freshly stored manifest.
func (cs *ContentService) announceChunks(hashes []string) {
	if cs.node == nil {
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

// chunkReader streams manifest content, fetching chunks on demand. Every
// chunk arrives hash-verified (fetchFrom checks it) and must match the length
// the manifest implies, so the reader yields exactly TotalSize correct bytes
// or fails.
type chunkReader struct {
	manifest *ChunkManifest
	fetch    func(hash string) ([]byte, error)
	next     int    // index of the next chunk to fetch
	cur      []byte // unread remainder of the current chunk
}

func (cr *chunkReader) Read(p []byte) (int, error) {
	for len(cr.cur) == 0 {
		if cr.next >= len(cr.manifest.Chunks) {
			return 0, io.EOF
		}
		data, err := cr.fetch(cr.manifest.Chunks[cr.next])
		if err != nil {
			return 0, fmt.Errorf("chunk %d/%d: %w", cr.next+1, len(cr.manifest.Chunks), err)
		}
		if int64(len(data)) != cr.manifest.chunkLen(cr.next) {
			return 0, fmt.Errorf("chunk %d/%d: length %d does not match manifest", cr.next+1, len(cr.manifest.Chunks), len(data))
		}
		cr.next++
		cr.cur = data
	}
	n := copy(p, cr.cur)
	cr.cur = cr.cur[n:]
	return n, nil
}

func (cr *chunkReader) Close() error { return nil }

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
	data, err := readBlob(limitReader(stream, cs.downLimit))
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
	cs.index.TouchBlob(hash) // being served keeps the set alive (TTL)
	if err := writeBlobHeader(stream, uint64(size)); err != nil {
		return
	}
	io.Copy(limitWriter(stream, cs.upLimit), rc)
}

// provideLoop re-announces every locally stored blob periodically so provider
// records stay fresh while this node is up.
func (cs *ContentService) provideLoop() {
	// Announce shortly after start (once peers are likely connected), then on a
	// steady interval via a single ticker (no per-iteration timer allocation).
	select {
	case <-time.After(20 * time.Second):
		cs.provideAll()
	case <-cs.node.ctx.Done():
		return
	}

	ticker := time.NewTicker(provideInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
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
