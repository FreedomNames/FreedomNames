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
- **Staying available**: a node re-announces every blob it stores on an interval,
  so an author's own site stays reachable while their node is up.

Blobs are whole (no chunking) in this version, capped at 32 MiB: comfortable for
pages plus reasonable assets. Larger media (chunking/DAG) is a later phase.

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
