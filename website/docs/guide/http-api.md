# HTTP API reference

Every node exposes an HTTP API (default `127.0.0.1:8420`) for publishing and
resolving records, moving content, and inspecting the node. The CLI talks to this
same API. The API binds to loopback by default; it is an unauthenticated local
control surface, so expose it beyond `127.0.0.1` only deliberately.

Owner-key operations use a second, loopback-only authoring origin (default
`127.0.0.1:8421`), advertised by `/health` when available.

Because it is unauthenticated, three classes of request are refused with `403`
before they reach any route, so a web page you merely visited cannot drive the
node behind your back:

- a `Host` header that is a **domain name** rather than `localhost` or an IP
  literal (this is how DNS rebinding reaches a service on `localhost`) — add
  yours to `FREEDOM_HTTP_ALLOWED_HOSTS` if you front the API with a hostname
  (with or without the port: both are matched);
- any request carrying `Sec-Fetch-Site: cross-site`, i.e. one another site
  caused your browser to make;
- a request whose `Origin` header names another site (cross-site request
  forgery).

Note that `GET` is **not** a safe method here: [`/content`](#post-get-content)
and [`/resolve-content`](#get-resolve-content) fetch from the network on a miss
and *keep* what they fetch, announcing this node to the DHT as a provider of it.
A plain `<img src="http://localhost:8420/content?hash=…">` on someone else's
page sends no `Origin` at all, so the `Sec-Fetch-Site` check is what stops a
page you visited from choosing what your node hosts and advertises.

`curl`, the `freedom` CLI and an embedding app send none of these headers, so
nothing in this reference changes for them; nor does typing a URL into the
address bar, or a page served from this same node.

| Route | Method | Purpose |
| --- | --- | --- |
| [`/publish`](#post-publish) | POST | Store a signed `FNRecord` |
| [`/resolve`](#get-resolve) | GET | Resolve a name to its records |
| [`/record`](#get-record) | GET | Fetch the raw signed record |
| [`/content`](#post-get-content) | POST/GET | Store / fetch page bytes by hash |
| [`/resolve-content`](#get-resolve-content) | GET | Name to page bytes in one call |
| [`/peers`](#get-peers) | GET | Routing-table peers + connected hosts |
| [`/info`](#get-info) | GET | Version, mode, peer ID, addresses, network size |
| [`/health`](#get-health) | GET | Liveness + version + role handshake |
| [`/clear_cache`](#delete-clear_cache) | DELETE | Purge the local resolution cache |
| [`/authoring/names`](#get-authoringnames) | GET/POST | List owned names or create an owner key (loopback only) |
| [`/authoring/names/<label>/publish`](#post-authoringnameslabelpublish) | POST | Build, sign and publish records (loopback only) |

## Local authoring API

The `/authoring/*` routes are a privileged management surface: they can use the
owner keys under `~/.freedom/keys/`. They therefore use a separate HTTP origin,
`127.0.0.1:8421` by default, from the ordinary API on port `8420`. This matters
because `/content` can serve user-controlled bytes: content opened on port
`8420` must never become same-origin with owner-key operations.

A request is accepted only when both the connection and its `Host` address are
loopback (`localhost`, `127.0.0.1`, or `::1`), and requests carrying proxy
forwarding headers or same-site/cross-site Fetch Metadata are rejected. The
listener refuses a non-loopback
`FREEDOM_AUTHORING_ADDR`, even if `FREEDOM_HTTP_ADDR` deliberately exposes the
ordinary API to a LAN.

Private key material is never returned. There is deliberately no arbitrary
"sign these bytes" endpoint: clients submit validated Freedom resource records,
and the node owns sequence selection, canonical record construction, signing,
and publication as one operation.

### GET `/authoring/names`

Lists the names whose owner keys exist locally, sorted by label:

```json
{
  "names": [
    {"label": "blog", "name": "blog.<pubKeyID>.fn"}
  ]
}
```

A damaged key remains visible by its label with an empty `name`, so it cannot
silently disappear and look available for replacement.

### POST `/authoring/names`

Creates a new Ed25519 owner key without overwriting an existing key:

```sh
curl -X POST http://localhost:8421/authoring/names \
  -H 'Content-Type: application/json' \
  -d '{"label":"blog"}'
```

Response `201 Created`:

```json
{"label":"blog","name":"blog.<pubKeyID>.fn"}
```

The key is stored as `~/.freedom/keys/blog.key` with mode `0600`. Errors are
structured as `{"error":"..."}`: `400` invalid JSON/label, `409` an owner key
already exists, `403` the request is not strictly local.

### POST `/authoring/names/<label>/publish`

Builds, signs and publishes a complete replacement record set:

```sh
curl -X POST http://localhost:8421/authoring/names/blog/publish \
  -H 'Content-Type: application/json' \
  -d '{"records":[{"type":"CONTENT","value":"<content-hash>","ttl":300}]}'
```

Response `200 OK`:

```json
{
  "published": "blog.<pubKeyID>.fn",
  "seq": 1720713600,
  "expires": 1721318400
}
```

`seq` and `expires` are Unix seconds. Publications are serialized per label;
the chosen sequence is strictly higher than the current record even when two
local clients publish during the same second. The request replaces the name's
whole record set—it does not merge with records already on the network.

Errors are structured JSON: `400` malformed or invalid records, `404` no local
owner key, `409` no newer sequence can be represented, `503` the DHT is not
ready, `502` the current network record could not be checked, and `403` the
request is not strictly local.

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
`405` for methods other than POST; `413` if the body exceeds 1 MiB; `500` if the
DHT isn't initialized or storage fails.

Verification enforces the [record size
limits](/guide/how-names-work#size-limits), so an oversized record set comes
back as `400`, not as a record the network refuses to carry later.

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
| `type` | no | filter to one type (`A`\|`AAAA`\|`TXT`\|`CNAME`\|`CONTENT`) |

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
exist (including a bare name that is unclaimed on
[bare names](/guide/bare-names)); `500` if the DHT isn't initialized yet; `502` if the
lookup infrastructure failed (DHT timeout, no peers, Electrum unreachable),
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

**Errors:** `500` if the DHT isn't initialized yet (also true for `/info`).

## GET `/info`

Returns general information about the node and its view of the network.

```sh
curl http://localhost:8420/info
```

```json
{
  "version": "<version>",
  "role": "node",
  "mode": "Auto",
  "peerID": "<peerID>",
  "listenAddresses": ["/ip4/…/tcp/…", "..."],
  "peers": ["<peerID>", "..."],
  "hostsConnected": 3,
  "networkSize": 42,
  "protocols": ["/ipfs/kad/1.0.0", "..."]
}
```

**Errors:** `500` if the DHT isn't initialized yet.

## DELETE `/clear_cache`

Purges the node's local resolution cache. Useful after publishing an update if you
want a resolve to skip the cache and hit the DHT.

```sh
curl -X DELETE http://localhost:8420/clear_cache
```

Returns `200 OK` with an empty body; any method other than DELETE gets a `405`.

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

The node stores the bytes locally, announces a provider record, and pushes
replicas to the closest peers in the background (see
[replication](/guide/content#replication-distributed-by-design)). Content
larger than 8 MiB is transparently split into chunks plus a manifest (the
returned hash addresses the manifest); the body is consumed as a stream. Max
content size is 1 GiB.

**Fetch**: `GET` with `?hash=`:

```sh
curl "http://localhost:8420/content?hash=muf...hbst" -o page.html
```

Returns the raw bytes (`application/octet-stream`, with `Content-Length`) from
the local store, or fetched from a provider on a miss. Chunked content is
streamed chunk by chunk as it is fetched. Received bytes are verified against
their hashes.

**Errors:** `400` missing/invalid hash; `404` not found on the network; `405`
for methods other than POST/GET; `413` if a stored body exceeds the 1 GiB max;
`500` if storing fails locally; `502` transient discovery/transfer failure;
`503` content service disabled.

## GET `/resolve-content`

Resolve a name to its `CONTENT` record and stream the bytes in one call: the
request a browser makes per page load.

```sh
curl "http://localhost:8420/resolve-content?name=blog.<pubKeyID>.fn" -o page.html
```

Returns the raw page bytes; the content hash is echoed in the
`X-Freedom-Content-Hash` response header.

**Errors:** `400` missing name; `404` name has no CONTENT record or content
unavailable; `502` transient failure; `503` content service disabled.

## GET `/health`

A stable liveness + version endpoint for a spawning host to confirm the node is
up and is the expected build.

```sh
curl http://localhost:8420/health
```

```json
{
  "status": "ok",
  "version": "<version>",
  "ready": true,
  "role": "node",
  "capabilities": ["authoring"],
  "authoringAPI": "http://127.0.0.1:8421"
}
```

The endpoint answers any HTTP method, always with `200`; `ready` flips to `true`
once the DHT is initialized.

`role` is one of a fixed vocabulary, not free text:

| value | meaning |
|---|---|
| `node` | a normal node (DNS + HTTP API, default HTTP port `8420`) |
| `bootstrap` | a bootstrap node (fixed p2p ports, no DNS, default HTTP port `8430`) |

**`role` is the supported way for a spawning host to tell node types apart.** Do
not infer the type from listen ports: a bootstrap node can be configured onto
different ports, and `mode` does not identify one either (a normal node on `Auto`
is promoted to `Server` once it is publicly reachable).

The field is **always present, on every response, including while `ready` is
still `false`**. That guarantee is deliberate: `/info` returns `500` until the
DHT is initialized, so a host that probed `/info` could read a still-starting
node as "nothing listening here" and start a second one. `/health` always
answers.

`capabilities` is the supported feature-discovery mechanism for embedding
applications. Check for `authoring` before presenting owner-name operations;
this lets an application safely adopt an older running node while retaining
content-hash-only publishing. Use the accompanying `authoringAPI` URL rather
than assuming a port. Bootstrap nodes and nodes whose authoring listener could
not start omit the capability and URL.

## Next

- The [**CLI**](/guide/cli) that wraps this API.
- The [**content network**](/guide/content) and [**embedding a node**](/guide/embedding).
- [**Configuration**](/guide/configuration) to change the listen address and more.
