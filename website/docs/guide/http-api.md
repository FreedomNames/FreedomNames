# HTTP API reference

Every node exposes an HTTP API (default `:8080`) for publishing and resolving
records and for inspecting the node. The CLI talks to this same API.

| Route | Method | Purpose |
| --- | --- | --- |
| [`/publish`](#post-publish) | POST | Store a signed `FNRecord` |
| [`/resolve`](#get-resolve) | GET | Resolve a name to its records |
| [`/peers`](#get-peers) | GET | Routing-table peers + connected hosts |
| [`/info`](#get-info) | GET | Node mode, peer ID, addresses, network size |
| [`/clear_cache`](#delete-clear_cache) | DELETE | Purge the local resolution cache |

## POST `/publish`

Stores a **pre-signed** `FNRecord` (JSON body) in the DHT. The client is expected
to have signed the record with the owner's private key — the `freedom publish`
command does this for you. The node verifies the record before storing it and
rejects anything unowned, forged, expired, or malformed.

**Request body** — a signed `FNRecord`:

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
You rarely POST this by hand — signing requires the private key. Use
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
curl "http://localhost:8080/resolve?name=mysite.<pubKeyID>.fn&type=A"
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

**Errors:** `400` if `name` is missing; `404` if the name can't be resolved.

## GET `/peers`

Returns the DHT routing-table peers and the currently connected hosts.

```sh
curl http://localhost:8080/peers
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
curl http://localhost:8080/info
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
curl -X DELETE http://localhost:8080/clear_cache
```

Returns `200 OK` with an empty body.

## Next

- The [**CLI**](/guide/cli) that wraps this API.
- [**Configuration**](/guide/configuration) — change the listen address and more.
