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
{ "status": "ok", "version": "0.8.1", "ready": true }
```

`version` lets the host confirm it launched the build it shipped; `ready` becomes
true once the DHT and host are initialized.

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
Layer 2, globally-unique bare names) and content transport in one small binary,
with the same content-addressing guarantee: a hash always yields exactly those
bytes, or nothing.

## Next

- The [**content network**](/guide/content) in depth.
- The [**HTTP API**](/guide/http-api) reference.
