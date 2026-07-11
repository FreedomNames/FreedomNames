# HTTP API reference

Every node exposes an HTTP API (default `127.0.0.1:8420`) for publishing and
resolving records, moving content, and inspecting the node. The CLI talks to this
same API. The API binds to loopback by default; it is an unauthenticated local
control surface, so expose it beyond `127.0.0.1` only deliberately.

| Route | Method | Purpose |
| --- | --- | --- |
| [`/publish`](#post-publish) | POST | Store a signed `FNRecord` |
| [`/resolve`](#get-resolve) | GET | Resolve a name to its records |
| [`/record`](#get-record) | GET | Fetch the raw signed record |
| [`/content`](#post-get-content) | POST/GET | Store / fetch page bytes by hash |
| [`/resolve-content`](#get-resolve-content) | GET | Name to page bytes in one call |
| [`/peers`](#get-peers) | GET | Routing-table peers + connected hosts |
| [`/info`](#get-info) | GET | Version, mode, peer ID, addresses, network size |
| [`/health`](#get-health) | GET | Liveness + version handshake |
| [`/clear_cache`](#delete-clear_cache) | DELETE | Purge the local resolution cache |

## POST `/publish`

Stores a **pre-signed** `FNRecord` (JSON body) in the DHT. The client is expected
to have signed the record with the owner's private key; the `freedom publish`
command does this for you. The node verifies the record before storing it and
rejects anything unowned, forged, expired, or malformed.

**Request body**, a signed `FNRecord`:

```json
{
  "label": "mysite",
  "records": [
    { "type": "A", "value": "10.0.0.5", "ttl": 300 }
  ],
  "seq": 1720713600,
  "eol": 0,
  "pubKey": "<base64 marshaled Ed25519 public key>",
  "sig": "<base64 Ed25519 signature>"
}
```

**Response** `200 OK`:

```json
{ "published": "mysite.<pubKeyID>.fn" }
```

**Errors:** `400` if the body isn't a valid `FNRecord` or fails verification;
`500` if the DHT isn't initialized or storage fails.

::: tip
You rarely POST this by hand, since signing requires the private key. Use
[`freedom publish`](/guide/cli#freedom-publish-label-api-url), which builds and
signs the record for you.
:::

## GET `/resolve`

Resolves a full `label.<pubKeyID>.fn` name to its resource records, optionally
filtered by type.

**Query parameters:**

| Param | Required | Meaning |
| --- | --- | --- |
| `name` | yes | the full name to resolve |
| `type` | no | filter to one type (`A`\|`AAAA`\|`TXT`\|`CNAME`) |

```sh
curl "http://localhost:8420/resolve?name=mysite.<pubKeyID>.fn&type=A"
```

**Response** `200 OK`:

```json
{
  "name": "mysite.<pubKeyID>.fn",
  "records": [
    { "type": "A", "value": "10.0.0.5", "ttl": 300 }
  ]
}
```

**Errors:** `400` if `name` is missing or malformed; `404` if the name does not
exist; `501` for bare names (the Layer 2 registry is not built yet, do not
retry); `502` if the lookup infrastructure failed (DHT timeout, no peers),
which means: retry later, the name may still exist.

## GET `/record`

Returns the raw signed record for a name, including its sequence number and
expiry. Bypasses the resolution cache. The CLI uses this to pick the next
sequence number when publishing an update.

**Query parameters:**

| Param | Required | Meaning |
| --- | --- | --- |
| `name` | yes | the full name to fetch |

```sh
curl "http://localhost:8420/record?name=mysite.<pubKeyID>.fn"
```

**Response** `200 OK`: the full `FNRecord` JSON (`label`, `records`, `seq`,
`eol`, `pubKey`, `sig`).

**Errors:** same status mapping as `/resolve`.

## GET `/peers`

Returns the DHT routing-table peers and the currently connected hosts.

```sh
curl http://localhost:8420/peers
```

```json
{
  "peers": ["<peerID>", "..."],
  "hosts": ["<peerID>", "..."]
}
```

## GET `/info`

Returns general information about the node and its view of the network.

```sh
curl http://localhost:8420/info
```

```json
{
  "mode": "client",
  "peerID": "<peerID>",
  "listenAddresses": ["/ip4/…/tcp/…", "..."],
  "peers": ["<peerID>", "..."],
  "hostsConnected": 3,
  "networkSize": 42,
  "protocols": ["/ipfs/kad/1.0.0", "..."]
}
```

## DELETE `/clear_cache`

Purges the node's local resolution cache. Useful after publishing an update if you
want a resolve to skip the cache and hit the DHT.

```sh
curl -X DELETE http://localhost:8420/clear_cache
```

Returns `200 OK` with an empty body.

## POST / GET `/content`

The content layer's store and fetch. See [the content network](/guide/content)
for the model.

**Store**: `POST` raw bytes (`application/octet-stream`):

```sh
curl -X POST --data-binary @index.html http://localhost:8420/content
```

```json
{ "hash": "muf...hbst" }
```

The node stores the bytes locally and announces a provider record. Max body size
is 32 MiB.

**Fetch**: `GET` with `?hash=`:

```sh
curl "http://localhost:8420/content?hash=muf...hbst" -o page.html
```

Returns the raw bytes (`application/octet-stream`) from the local store, or
fetched from a provider on a miss. Received bytes are verified against the hash.

**Errors:** `400` missing/invalid hash; `404` not found on the network; `502`
transient discovery/transfer failure; `503` content service disabled.

## GET `/resolve-content`

Resolve a name to its `CONTENT` record and stream the bytes in one call: the
request a browser makes per page load.

```sh
curl "http://localhost:8420/resolve-content?name=blog.<pubKeyID>.fn" -o page.html
```

Returns the raw page bytes; the content hash is echoed in the
`X-Freedom-Content-Hash` response header.

**Errors:** `400` missing name; `404` name has no CONTENT record or content
unavailable; `502` transient failure.

## GET `/health`

A stable liveness + version endpoint for a spawning host to confirm the node is
up and is the expected build.

```sh
curl http://localhost:8420/health
```

```json
{ "status": "ok", "version": "0.3.0", "ready": true }
```

## Next

- The [**CLI**](/guide/cli) that wraps this API.
- The [**content network**](/guide/content) and [**embedding a node**](/guide/embedding).
- [**Configuration**](/guide/configuration) to change the listen address and more.
