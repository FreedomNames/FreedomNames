# The content network

Names are only half of a website. A `.fn` name resolves to *records*, but a page
also has *bytes*: HTML, markdown, images. Freedom Names carries those bytes too,
over a peer-to-peer content layer. This is what lets it replace IPFS as a
browser's whole backend.

## Names, records, and content

Three layers stack cleanly:

1. **Name → records** (the DHT): `mysite.<pubKeyID>.fn` resolves to a signed
   record set.
2. **Record → content hash** (a `CONTENT` record): one of those records is
   `CONTENT <hash>`, where `<hash>` is the content-address of the page bytes.
3. **Content hash → bytes** (the content network): the bytes are fetched from
   whichever peer holds them.

The DHT never stores the bytes: it is capped at small values and is for naming
and *discovery* only. The bytes travel over a dedicated stream protocol.

## How content moves

- **Storing**: `POST /content` (or `freedom put`) hashes the bytes with sha2-256
  (the same multihash format as a `pubKeyID`), stores them in a local
  content-addressed blobstore (`~/.freedom/content`, overridable with
  `FREEDOM_CONTENT_DIR`), and announces a **provider record** to the DHT so
  other peers know this node holds that hash.
- **Fetching**: `GET /content?hash=` returns the bytes from the local store, or -
  on a miss: asks the DHT *which peers* hold the hash, dials one, and streams the
  blob over `/freedomnames/content/1.0.0`. The received bytes are verified against
  the requested hash — a peer cannot serve you the wrong content; a bad response
  is discarded and the next provider tried — and cached locally.
- **Staying available**: content is **replicated by design, at publish time** —
  not on demand, and with no pinning. See the next section.

## Replication: distributed by design

Freedom Names assumes every node goes down eventually — including the
publisher's. So availability is never left to demand (the IPFS trade-off,
where content nobody fetched lives only on the publisher's node until someone
pins it). Instead, the swarm holds the data:

- **Push at publish**: storing content immediately pushes full copies to the 3
  peers closest (in DHT distance) to the content's hash, over
  `/freedomnames/content/push/1.0.0`. A receiver accepts a push
  all-or-nothing: every blob must match its hash, chunks must arrive in
  manifest order with the lengths the manifest implies, and the byte total
  must equal the offer — anything less and nothing is stored or announced. A
  peer that already holds the set answers "have" without any bytes moving;
  that still counts toward the replica target and refreshes the set's TTL.
  From the first minute, the publisher is not a single point of failure.
- **Self-healing**: every holder — publisher or replica — periodically counts
  the live providers of each content set it holds. If the count fell below the
  target (holders die, disks fail), it pushes copies to new closest peers.
  This runs on *whoever still holds the content*, so replication keeps
  healing even after the publisher is gone for good. (A node only heals once
  it is bootstrapped into the network, and each holder's first pass is
  randomly delayed within the interval so the swarm doesn't heal in
  lockstep.)
- **Spread on demand too**: a node that fetches content keeps a copy (budget
  permitting) and becomes one more provider, so popular content grows extra
  replicas beyond the target. When the budget is full the fetch still
  succeeds — the bytes are served without being cached, and the node simply
  doesn't become a provider.

Every holder also re-announces its provider records every 12 hours, for hosted
replicas as much as for its own content. DHT provider records expire on their
own after a day or two, so content stays discoverable only while at least one
holder is (at least occasionally) online.

Node operators stay in control of what they contribute:

| Setting | Default | Meaning |
| --- | --- | --- |
| `FREEDOM_CONTENT_REPLICAS` | `3` | copies pushed per publish (target holders = this + 1) |
| `FREEDOM_CONTENT_HOST_BUDGET` | `20GB` | max disk spent hosting other people's content |
| `FREEDOM_CONTENT_HOST_TTL` | `720h` (30 days) | hosted content untouched for this long loses its eviction protection — it is **not** deleted until space is needed |
| `FREEDOM_CONTENT_HEAL_INTERVAL` | `1h` | how often replica counts are checked and topped up (`0` disables healing) |
| `FREEDOM_CONTENT_UP_RATE` | unlimited | bytes/second serving + pushing content (e.g. `10MB`) |
| `FREEDOM_CONTENT_DOWN_RATE` | unlimited | bytes/second fetching + receiving pushes |
| `FREEDOM_CONTENT_MAX_PUSH_SIZE` | `1GB` | largest single content set accepted from a push (1 GiB is also a hard wire-level ceiling — setting this higher has no effect) |

Rate limits apply only to bulk content transfer; DHT and naming traffic are
never limited. Low rates still allow a minimum burst of 64 KiB.

Your **own published content is never evicted** and never counts against the
hosting budget. Hosted content (pushed to you, or cached from your fetches) is
only ever removed to make room: while the budget has space, nothing is deleted
— not even TTL-expired sets. This is deliberate: every replica a node keeps is
content the network can still serve, so a node never *proactively* destroys
availability — there is no cleanup timer, only eviction priority at the moment
space is genuinely needed. When a new set needs room, eviction picks
TTL-expired sets first (least recently accessed first) and falls back to plain
LRU. Any access or re-push from a healing peer refreshes a set's clock, so
content with a living swarm effectively never expires. Note that publishing an
*updated* page is a new content set with a new hash: the old version remains
owned (and so pinned) locally until you delete its blobs by hand.

The accounting lives in a small `index.json` next to the blobs; blobs already
on disk from older versions are adopted as hosted content on first start
(re-publish once to mark them owned). A chunked set is only adopted whole when
its manifest and every chunk are present — and only manifests up to 1 MiB are
recognized as such; anything else is adopted as a plain single blob.

## Large content: chunking

Content up to 8 MiB is a single blob whose hash is simply the hash of its
bytes. Larger content (up to 1 GiB) is transparently split into 8 MiB chunks —
each an ordinary blob — plus a small **manifest** blob listing the chunk hashes
in order. The `CONTENT` record then points at the manifest's hash.

Fetching is the same machinery applied twice: get the manifest (from the local
store or a provider), then stream each chunk, preferring the peer that served
the manifest before falling back to per-chunk provider discovery. Every chunk
is verified against its own hash and its length checked against the manifest,
so a peer can neither corrupt nor truncate the content undetected. Assembly is
streaming — only one chunk is held in memory at a time, on both the storing
and the fetching side. (The blobstore itself caps any single blob at a hard
32 MiB; with 8 MiB chunks that ceiling is never reached in practice.)

## Publishing a page in one step

`freedom put` is the author's shortcut: upload a file, point a name at it, and
publish, all at once.

```sh
freedom keygen blog                       # your owner key
freedom put blog ./index.html             # upload + set CONTENT record + publish
```

```
Uploaded ./index.html (2048 bytes) -> muf...hbst
Published blog.<pubKeyID>.fn (seq ..., 1 record(s))
Record valid until ...
```

Under the hood `put` does roughly what you could do by hand:

```sh
HASH=$(curl -s -X POST --data-binary @index.html http://localhost:8420/content | jq -r .hash)
freedom set blog CONTENT "$HASH"
freedom publish blog
```

With one difference: where `freedom set` merges into the staged record set,
`put` **replaces** it — the name ends up with a single `CONTENT` record (TTL
300 unless `--ttl` says otherwise). If the name should also carry `A` or `TXT`
records, use the by-hand sequence instead.

## Reading a page

The whole point is one request per page load. `GET /resolve-content?name=` does
the full chain: resolve the name, read its `CONTENT` record (if a set carries
several, the first one the resolver returns is served), fetch and stream the
bytes:

```sh
curl "http://localhost:8420/resolve-content?name=blog.<pubKeyID>.fn"
```

The response is the raw page bytes (`application/octet-stream`), with the content
hash echoed in the `X-Freedom-Content-Hash` header. The exact `Content-Length`
is sent up front; if a chunk fetch fails mid-stream the response can only be
truncated (the success status is already on the wire), which a client detects
by the length mismatch.

## Next

- The [**HTTP API**](/guide/http-api) reference for `/content` and
  `/resolve-content`.
- [**Embedding a node**](/guide/embedding) in a browser or app.
