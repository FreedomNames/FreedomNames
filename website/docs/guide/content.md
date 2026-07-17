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
  content-addressed blobstore (`~/.freedom/content`), and announces a **provider
  record** to the DHT so other peers know this node holds that hash.
- **Fetching**: `GET /content?hash=` returns the bytes from the local store, or -
  on a miss: asks the DHT *which peers* hold the hash, dials one, and streams the
  blob over `/freedomnames/content/1.0.0`. The received bytes are verified against
  the requested hash (a peer cannot serve you the wrong content) and cached
  locally.
- **Staying available**: content is **replicated by design, at publish time** —
  not on demand, and with no pinning. See the next section.

## Replication: distributed by design

Freedom Names assumes every node goes down eventually — including the
publisher's. So availability is never left to demand (the IPFS trade-off,
where content nobody fetched lives only on the publisher's node until someone
pins it). Instead, the swarm holds the data:

- **Push at publish**: storing content immediately pushes full copies to the 3
  peers closest (in DHT distance) to the content's hash, over
  `/freedomnames/content/push/1.0.0`. Each receiver verifies every byte
  against the content hashes before storing, then announces itself as a
  provider. From the first minute, the publisher is not a single point of
  failure.
- **Self-healing**: every holder — publisher or replica — periodically counts
  the live providers of each content set it holds. If the count fell below the
  target (holders die, disks fail), it pushes copies to new closest peers.
  This runs on *whoever still holds the content*, so replication keeps
  healing even after the publisher is gone for good.
- **Spread on demand too**: a node that fetches content keeps a copy (budget
  permitting) and becomes one more provider, so popular content grows extra
  replicas beyond the target.

Node operators stay in control of what they contribute:

| Setting | Default | Meaning |
| --- | --- | --- |
| `FREEDOM_CONTENT_REPLICAS` | `3` | copies pushed per publish (target holders = this + 1) |
| `FREEDOM_CONTENT_HOST_BUDGET` | `20GB` | max disk spent hosting other people's content |
| `FREEDOM_CONTENT_HOST_TTL` | `720h` (30 days) | hosted content untouched for this long loses its eviction protection — it is **not** deleted until space is needed |
| `FREEDOM_CONTENT_HEAL_INTERVAL` | `1h` | how often replica counts are checked and topped up (`0` disables healing) |
| `FREEDOM_CONTENT_UP_RATE` | unlimited | bytes/second serving + pushing content (e.g. `10MB`) |
| `FREEDOM_CONTENT_DOWN_RATE` | unlimited | bytes/second fetching + receiving pushes |
| `FREEDOM_CONTENT_MAX_PUSH_SIZE` | `1GB` | largest single content set accepted from a push |

Your **own published content is never evicted** and never counts against the
hosting budget. Hosted content (pushed to you, or cached from your fetches) is
only ever removed to make room: while the budget has space, nothing is deleted
— not even TTL-expired sets. When a new set needs room, eviction picks
TTL-expired sets first (least recently accessed first) and falls back to plain
LRU. Any access or re-push from a healing peer refreshes a set's clock, so
content with a living swarm effectively never expires. The accounting lives in
a small `index.json` next to the blobs; blobs already on disk from older
versions are adopted as hosted content on first start (re-publish once to mark
them owned).

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
and the fetching side.

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

Under the hood `put` does what you could do by hand:

```sh
HASH=$(curl -s -X POST --data-binary @index.html http://localhost:8420/content | jq -r .hash)
freedom set blog CONTENT "$HASH"
freedom publish blog
```

## Reading a page

The whole point is one request per page load. `GET /resolve-content?name=` does
the full chain: resolve the name, read its `CONTENT` record, fetch and stream the
bytes:

```sh
curl "http://localhost:8420/resolve-content?name=blog.<pubKeyID>.fn"
```

The response is the raw page bytes (`application/octet-stream`), with the content
hash echoed in the `X-Freedom-Content-Hash` header.

## Next

- The [**HTTP API**](/guide/http-api) reference for `/content` and
  `/resolve-content`.
- [**Embedding a node**](/guide/embedding) in a browser or app.
