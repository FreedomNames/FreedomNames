package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// This file makes content distribution proactive: a publish PUSHES copies of
// the content to other nodes instead of waiting for demand, and every holder
// periodically tops the replica count back up (healing). Content therefore
// survives any node going down — including the publisher — by design, with no
// pinning. Placement follows Kademlia: the peers closest to the content's
// root hash host it, so all healers converge on the same target set.

// pushProtocol transfers a whole content set (a blob, or manifest + chunks)
// to one peer in a single session.
const pushProtocol = protocol.ID("/freedomnames/content/push/1.0.0")

// Push replies: the receiver's one-byte answer to an offer.
const (
	pushDecline byte = 0 // policy refused (budget/size); pusher tries another peer
	pushAccept  byte = 1 // send the blobs
	pushHave    byte = 2 // already holding the set; counts as a live replica
)

// contentSet is the unit of replication: the root blob plus, for chunked
// content, every chunk. size is the total bytes across all blobs.
type contentSet struct {
	root   string
	size   int64
	chunks []string // nil for single-blob content
}

// loadContentSet resolves a root hash to its full set from the local store.
func (cs *ContentService) loadContentSet(root string) (*contentSet, error) {
	data, err := cs.store.Get(root)
	if err != nil {
		return nil, err
	}
	set := &contentSet{root: root, size: int64(len(data))}
	if m, ok := decodeManifest(data); ok {
		set.chunks = m.Chunks
		set.size += m.TotalSize
	}
	return set, nil
}

// numBlobs is 1 for plain content, 1 + chunk count for chunked content.
func (s *contentSet) numBlobs() uint64 { return uint64(1 + len(s.chunks)) }

// pushDeadline scales a push session's deadline with the bytes to move
// (baseline 30s + 1s per MiB, capped at 10 minutes).
func pushDeadline(size int64) time.Duration {
	d := 30*time.Second + time.Duration(size>>20)*time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// --- wire format ---
//
// Offer:    varint-len + root hash | uvarint total size | uvarint blob count
// Reply:    1 status byte (decline/accept/have)
// Blobs:    per blob: varint-len + hash | uvarint size + raw bytes
//           (root/manifest first, then chunks in manifest order)
// Final:    1 byte from the receiver: 1 = all verified and stored, 0 = failed

func writePushOffer(w io.Writer, root string, size, nblobs uint64) error {
	if err := writeRequest(w, root); err != nil {
		return err
	}
	if err := writeBlobHeader(w, size); err != nil {
		return err
	}
	return writeBlobHeader(w, nblobs)
}

func readPushOffer(r io.Reader) (root string, size, nblobs uint64, err error) {
	root, err = readRequest(r)
	if err != nil {
		return "", 0, 0, err
	}
	if !isContentHash(root) {
		return "", 0, 0, fmt.Errorf("push offer: invalid root hash")
	}
	br := newByteReaderFrom(r)
	if size, err = binary.ReadUvarint(br); err != nil {
		return "", 0, 0, err
	}
	if size == 0 || size > maxContentSize {
		return "", 0, 0, fmt.Errorf("push offer: bad size %d", size)
	}
	if nblobs, err = binary.ReadUvarint(br); err != nil {
		return "", 0, 0, err
	}
	if nblobs == 0 || nblobs > 1+maxManifestChunks {
		return "", 0, 0, fmt.Errorf("push offer: bad blob count %d", nblobs)
	}
	return root, size, nblobs, nil
}

// pushTo offers a content set to one peer and, if accepted, streams every
// blob. Returns the receiver's status byte (pushHave counts as success for
// replication purposes).
func (cs *ContentService) pushTo(ctx context.Context, p peer.ID, set *contentSet) (byte, error) {
	streamCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stream, err := cs.node.kadDHT.Host().NewStream(streamCtx, p, pushProtocol)
	if err != nil {
		return pushDecline, fmt.Errorf("open push stream to %s: %w", p, err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(pushDeadline(set.size)))
	return cs.pushOnStream(stream, set)
}

// pushOnStream runs the pusher's side of the protocol on an already-open
// stream (separated from dialing so it tests over any transport).
func (cs *ContentService) pushOnStream(stream io.ReadWriter, set *contentSet) (byte, error) {
	if err := writePushOffer(stream, set.root, uint64(set.size), set.numBlobs()); err != nil {
		return pushDecline, err
	}
	status, err := readStatusByte(stream)
	if err != nil {
		return pushDecline, err
	}
	if status != pushAccept {
		return status, nil
	}

	w := limitWriter(stream, cs.upLimit)
	blobs := append([]string{set.root}, set.chunks...)
	for _, h := range blobs {
		rc, size, err := cs.store.Open(h)
		if err != nil {
			return pushDecline, fmt.Errorf("push %s: local blob %s: %w", set.root, h, err)
		}
		err = func() error {
			defer rc.Close()
			if err := writeRequest(stream, h); err != nil {
				return err
			}
			if err := writeBlobHeader(stream, uint64(size)); err != nil {
				return err
			}
			_, err := io.Copy(w, rc)
			return err
		}()
		if err != nil {
			return pushDecline, err
		}
	}

	final, err := readStatusByte(stream)
	if err != nil {
		return pushDecline, err
	}
	if final != 1 {
		return pushDecline, errors.New("receiver failed to store the set")
	}
	return pushAccept, nil
}

// handlePushStream serves an inbound push: apply the hosting policy to the
// offer, then receive and verify each blob against the hashes the root
// commits to. Nothing is indexed (and so nothing is announced) unless the
// whole set verifies.
func (cs *ContentService) handlePushStream(stream network.Stream) {
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(30 * time.Second))

	root, size, nblobs, err := readPushOffer(stream)
	if err != nil {
		return
	}

	if cs.index.Has(root) {
		cs.index.Touch(root) // a live re-push refreshes the TTL
		stream.Write([]byte{pushHave})
		return
	}
	// Reserve rather than merely check: the bytes do not land until the
	// transfer finishes, and concurrent pushes would otherwise all be admitted
	// against the same pre-transfer usage and blow past the hosting budget.
	// Reserving deletes nothing — see ContentIndex.Reserve.
	if !cs.reserveHosted(int64(size)) {
		stream.Write([]byte{pushDecline})
		return
	}
	reserved := true
	defer func() {
		if reserved {
			cs.releaseHosted(int64(size))
		}
	}()
	if _, err := stream.Write([]byte{pushAccept}); err != nil {
		return
	}
	stream.SetDeadline(time.Now().Add(pushDeadline(int64(size))))

	r := limitReader(stream, cs.downLimit)
	var (
		manifest *ChunkManifest
		received uint64
	)
	fail := func(why string, args ...any) {
		log.Printf("content: reject push %s from %s: %s", root, stream.Conn().RemotePeer(), fmt.Sprintf(why, args...))
		stream.Write([]byte{0})
	}
	for i := uint64(0); i < nblobs; i++ {
		hash, err := readRequest(r)
		if err != nil {
			fail("read blob header: %v", err)
			return
		}
		data, err := readBlob(r)
		if err != nil {
			fail("read blob %s: %v", hash, err)
			return
		}
		got, err := contentHash(data)
		if err != nil || got != hash {
			fail("blob does not match its hash")
			return
		}
		if i == 0 {
			if hash != root {
				fail("first blob %s is not the offered root", hash)
				return
			}
			m, isManifest := decodeManifest(data)
			if isManifest {
				if nblobs != uint64(1+len(m.Chunks)) {
					fail("blob count %d does not match manifest (%d chunks)", nblobs, len(m.Chunks))
					return
				}
				manifest = m
			} else if nblobs != 1 {
				fail("blob count %d for non-manifest root", nblobs)
				return
			}
		} else {
			if hash != manifest.Chunks[i-1] {
				fail("chunk %d out of order", i)
				return
			}
			if int64(len(data)) != manifest.chunkLen(int(i-1)) {
				fail("chunk %d length mismatch", i)
				return
			}
		}
		if _, err := cs.store.Put(data); err != nil {
			fail("store blob: %v", err)
			return
		}
		received += uint64(len(data))
	}
	if received != size {
		fail("received %d bytes, offered %d", received, size)
		return
	}

	var chunks []string
	if manifest != nil {
		chunks = manifest.Chunks
	}
	// The bytes are here and verified: this is where making room may finally
	// delete something. commitHosted consumes the reservation either way.
	reserved = false
	if !cs.commitHosted(root, int64(size), chunks, stream.Conn().RemotePeer().String()) {
		fail("hosting budget filled while the transfer was in flight")
		return
	}
	// This node is now a holder: make that discoverable right away.
	if cs.node != nil {
		go func() {
			cs.announce(cs.node.ctx, root)
			cs.announceChunks(chunks)
		}()
	}
	stream.Write([]byte{1})
}

// admitHosted applies the operator's hosting policy (budget, per-set cap,
// TTL-driven eviction) to a new hosted set of the given size. Use it when the
// bytes are already in hand; for a transfer that has yet to arrive, use
// reserveHosted so the pending bytes count against the budget meanwhile.
func (cs *ContentService) admitHosted(size int64) bool {
	if cs.index == nil {
		return true // store-only service (tests): no policy
	}
	return cs.index.Admit(size, cs.hostBudget, cs.maxPushSize, cs.hostTTL, time.Now())
}

// reserveHosted admits a set and holds its size against the budget until
// releaseHosted is called. Every successful reserve needs exactly one release.
func (cs *ContentService) reserveHosted(size int64) bool {
	if cs.index == nil {
		return true // store-only service (tests): no policy
	}
	return cs.index.Reserve(size, cs.hostBudget, cs.maxPushSize, cs.hostTTL, time.Now())
}

// releaseHosted drops a reservation taken by reserveHosted whose transfer never
// completed.
func (cs *ContentService) releaseHosted(size int64) { cs.index.Release(size) }

// commitHosted records a fully received set, evicting if the budget needs it.
// It consumes the reservation whether or not it succeeds.
func (cs *ContentService) commitHosted(root string, size int64, chunks []string, from string) bool {
	if cs.index == nil {
		return true // store-only service (tests): no policy
	}
	return cs.index.CommitHosted(root, size, chunks, from, cs.hostBudget, cs.maxPushSize, cs.hostTTL, time.Now())
}

func readStatusByte(r io.Reader) (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

// --- placement + healing ---

// replicator decides WHERE content goes and keeps replica counts topped up.
// The DHT interactions are function-valued seams so the decision logic tests
// without a network (the fakeDHT pattern).
type replicator struct {
	self     peer.ID
	replicas int // pushed copies R; target live holders = R + 1

	closest   func(ctx context.Context, root string, n int) ([]peer.ID, error)
	providers func(ctx context.Context, root string, max int) ([]peer.ID, error)
	push      func(ctx context.Context, p peer.ID, root string) (byte, error)
}

// replicate pushes a freshly published set toward the R closest peers,
// returning how many replicas were placed (or already existed there).
func (r *replicator) replicate(ctx context.Context, root string) int {
	peers, err := r.closest(ctx, root, r.replicas*2+2)
	if err != nil {
		log.Printf("content: replicate %s: closest peers: %v", root, err)
		return 0
	}
	placed := 0
	for _, p := range peers {
		if placed >= r.replicas {
			break
		}
		if p == r.self {
			continue
		}
		status, err := r.push(ctx, p, root)
		if err != nil {
			log.Printf("content: replicate %s to %s: %v", root, p, err)
			continue
		}
		if status == pushAccept || status == pushHave {
			placed++
		}
	}
	return placed
}

// newReplicator wires a replicator to the real DHT: closeness and provider
// lookups on the root CID's multihash (the same key Provide uses), pushes
// over the push protocol.
func (cs *ContentService) newReplicator(replicas int) *replicator {
	return &replicator{
		self:     cs.node.kadDHT.Host().ID(),
		replicas: replicas,
		closest: func(ctx context.Context, root string, n int) ([]peer.ID, error) {
			c, err := hashToCID(root)
			if err != nil {
				return nil, err
			}
			return cs.node.kadDHT.GetClosestPeers(ctx, string(c.Hash()))
		},
		providers: func(ctx context.Context, root string, max int) ([]peer.ID, error) {
			c, err := hashToCID(root)
			if err != nil {
				return nil, err
			}
			pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			var ids []peer.ID
			for p := range cs.node.kadDHT.FindProvidersAsync(pctx, c, max) {
				if p.ID != "" {
					ids = append(ids, p.ID)
				}
			}
			return ids, nil
		},
		push: func(ctx context.Context, p peer.ID, root string) (byte, error) {
			set, err := cs.loadContentSet(root)
			if err != nil {
				return pushDecline, err
			}
			return cs.pushTo(ctx, p, set)
		},
	}
}

// healLoop keeps every held content set (owned and hosted alike) at its
// target replica count for as long as this node runs — so a swarm of holders
// sustains content indefinitely even after the publisher is gone. The initial
// delay is randomized so holders don't heal in lockstep.
func (cs *ContentService) healLoop() {
	delay := time.Duration(rand.Int63n(int64(cs.healInterval)))
	select {
	case <-time.After(delay):
	case <-cs.node.ctx.Done():
		return
	}
	for {
		cs.healAll()
		cs.index.Flush()
		select {
		case <-time.After(cs.healInterval):
		case <-cs.node.ctx.Done():
			return
		}
	}
}

// healAll runs one heal pass over all held sets in random order.
func (cs *ContentService) healAll() {
	if cs.node.kadDHT.RoutingTable().Size() == 0 {
		return // not bootstrapped into the network yet
	}
	roots := cs.index.Roots()
	rand.Shuffle(len(roots), func(i, j int) { roots[i], roots[j] = roots[j], roots[i] })
	for _, root := range roots {
		select {
		case <-cs.node.ctx.Done():
			return
		default:
		}
		if err := cs.rep.heal(cs.node.ctx, root); err != nil {
			log.Printf("content: heal %s: %v", root, err)
		}
	}
}

// heal counts live providers of a set and, if below target, pushes it to
// enough new closest peers to top the count back up.
func (r *replicator) heal(ctx context.Context, root string) error {
	target := r.replicas + 1 // holders including this node
	found, err := r.providers(ctx, root, target+2)
	if err != nil {
		return err
	}
	known := map[peer.ID]bool{r.self: true}
	holders := 1 // this node
	for _, p := range found {
		if !known[p] {
			known[p] = true
			holders++
		}
	}
	if holders >= target {
		return nil
	}

	peers, err := r.closest(ctx, root, target*2+2)
	if err != nil {
		return err
	}
	need := target - holders
	for _, p := range peers {
		if need <= 0 {
			break
		}
		if known[p] {
			continue
		}
		status, err := r.push(ctx, p, root)
		if err != nil {
			continue
		}
		if status == pushAccept || status == pushHave {
			need--
		}
	}
	return nil
}
