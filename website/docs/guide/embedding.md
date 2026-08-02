# Embedding a node

Freedom Names is designed to be the whole backend for a decentralized-web app:
names *and* content, one local binary. This page describes how a host application
spawns and drives a node, replacing an IPFS daemon. For a real example see
[LibreWeb](/guide/libreweb), and the [use cases](/guide/use-cases) for where this
fits.

## The model

The host application spawns `freedom-names` as a child process, then talks to it
over its local HTTP API. For every page load it makes **one** request:

```
GET http://127.0.0.1:8420/resolve-content?name=<the .fn name>
```

and receives the page bytes. The node handles everything behind that: resolving
the name, reading its `CONTENT` record, discovering a provider, and streaming the
bytes.

## Spawning the node

Launch with flags (flags override environment variables):

```sh
freedom-names \
  --api-bind 127.0.0.1 \
  --http-addr 127.0.0.1:8420 \
  --content-dir /path/to/app/content
```

- **`--api-bind 127.0.0.1`** (the default) keeps the HTTP API on loopback. The API
  is an unauthenticated local control surface: it must not be exposed on a LAN.
- **`--http-addr`** sets the full listen address if you need a specific port.
- **`--authoring-addr`** sets the separate loopback owner-key API address.
- **`--content-dir`** points the blobstore at app-managed storage.
- **`--dns-addr`** sets the DNS server port (or run without DNS; it is optional).

## Confirming the node is ready

Poll `/health` until it reports ready and the expected build:

```sh
curl http://127.0.0.1:8420/health
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

`version` lets the host confirm it launched the build it shipped; `ready` becomes
true once the DHT and host are initialized.

`role` tells the host **what kind of node answered**: `node` for a normal node,
`bootstrap` for a bootstrap node. Probe `/health` on the port you are about to
use before spawning:

- **No response**: nothing is there; start your node.
- **`role: "node"`**: a normal node is already running. Adopt it if you want to
  share, but remember you did not start it, so do not stop it on exit.
- **`role: "bootstrap"`**: do not adopt it; a bootstrap node runs no DNS and is
  not a general-purpose node. (Bootstrap nodes default to `8430`, so this only
  happens if one was deliberately pointed at your port.)
- **No `role` field**: a node older than 0.8.4. Treat it as a normal node.

Probe `/health`, not `/info`: `/info` returns `500` until the DHT is initialized,
so a still-starting node would look like no node at all, and you would start a
second one. `role` is always present on `/health`, even while `ready` is `false`.

Use `capabilities`, rather than comparing versions, to discover optional
integration surfaces. When `authoring` is present, send owner-key operations to
the accompanying `authoringAPI` origin. When it is absent (for example on an
older adopted node), keep content-hash publishing available and hide name
management.

## Loading a page

```
GET /resolve-content?name=blog.<pubKeyID>.fn
```

- **200**: the response body is the page bytes (`application/octet-stream`); the
  content hash is in `X-Freedom-Content-Hash`.
- **404**: the name has no `CONTENT` record, or the content is not available on
  the network.
- **502**: a transient discovery/transfer failure; retry.

All errors are JSON (`{"error":"..."}`) so the host can show a friendly message.

## Authoring from the host app

When the user publishes a page from an in-app editor, the host uploads and
points a name at it in one call chain:

```
POST http://127.0.0.1:8420/content
  raw page bytes -> { "hash": "<hash>" }

POST <authoringAPI>/authoring/names/<label>/publish
  { "records": [{ "type": "CONTENT", "value": "<hash>", "ttl": 300 }] }
  -> { "published": "<full-name>", "seq": ..., "expires": ... }
```

List existing names with `GET <authoringAPI>/authoring/names`, or create one
with `POST <authoringAPI>/authoring/names`. The authoring service selects the
next sequence number, constructs the canonical record, signs it with the local
owner key, and publishes it as one operation. The `freedom put` CLI shares the
same key and record-building implementation.

## Why this replaces IPFS

IPFS gives content transport (add/get blocks) but not naming, and pulls
in a large daemon. Freedom Names provides both naming (self-certifying names,
plus globally-unique bare names on Bitcoin Cash) and content transport in one
small binary, with the same content-addressing guarantee: a hash always yields
exactly those bytes, or nothing.

## Next

- The [**content network**](/guide/content) in depth.
- The [**HTTP API**](/guide/http-api) reference.
