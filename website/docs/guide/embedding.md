# Embedding a node (LibreWeb)

Freedom Names is designed to be the whole backend for a decentralized-web
browser: names *and* content, one local binary. This page describes how a host
application (such as the LibreWeb browser) spawns and drives a node, replacing an
IPFS daemon.

## The model

The browser spawns `freedom-names` as a child process, then talks to it over its
local HTTP API. For every page load it makes **one** request:

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
- **`--content-dir`** points the blobstore at app-managed storage.
- **`--dns-addr`** sets the DNS server port (or run without DNS; it is optional).

## Confirming the node is ready

Poll `/health` until it reports ready and the expected build:

```sh
curl http://127.0.0.1:8420/health
```

```json
{ "status": "ok", "version": "<version>", "ready": true, "role": "node" }
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

## Loading a page

```
GET /resolve-content?name=blog.<pubKeyID>.fn
```

- **200**: the response body is the page bytes (`application/octet-stream`); the
  content hash is in `X-Freedom-Content-Hash`.
- **404**: the name has no `CONTENT` record, or the content is not available on
  the network.
- **502**: a transient discovery/transfer failure; retry.

All errors are JSON (`{"error":"..."}`) so the browser can show a friendly page.

## Authoring from the browser

When the user publishes a page from an in-browser editor, the host uploads and
points a name at it in one call chain:

```
POST /content        (raw page bytes)      -> { "hash": "<hash>" }
POST /publish        (a signed CONTENT record for the name)
```

The `freedom put` CLI command does exactly this sequence and is a good reference
for the flow.

## Why this replaces IPFS

IPFS gave LibreWeb content transport (add/get blocks) but not naming, and pulled
in a large daemon. Freedom Names provides both naming (self-certifying and, via
the registry for globally-unique bare names) and content transport in one small binary,
with the same content-addressing guarantee: a hash always yields exactly those
bytes, or nothing.

## Next

- The [**content network**](/guide/content) in depth.
- The [**HTTP API**](/guide/http-api) reference.
