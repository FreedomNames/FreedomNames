# Architecture

A Freedom Names node is one binary containing the naming peer, content service,
DNS and HTTP interfaces, and optional bare-name registry integration. This page
shows how those runtime components fit together.

## Runtime components

Running `./freedom-names` starts all of these at once:

```
   dig / browser ──▶ DNS server ──▶ Resolver + cache
   CLI / curl ────▶ HTTP API ────▶ Resolver + cache

   Resolver + cache ──┬──▶ DHT peer ──▶ signed records
                      └──▶ BCH registry ──▶ owner key ──▶ DHT peer

   HTTP API ──▶ Content service ──▶ blobstore + libp2p peers
```

- **libp2p DHT peer**: the decentralized storage and resolution network. Signed
  records are stored under `/fn/<pubKeyID>` and served to other peers. Owned
  records are re-put every 8 hours so they outlive the DHT's ~36-hour record
  expiry (up to their signed 7-day `eol`).
- **Content service**: stores page bytes in the local content-addressed
  blobstore, advertises and fetches them over libp2p, accepts replica pushes,
  and repairs the configured replica count.
- **DNS server** (default `:8053`, no root needed): resolves `.fn` names through
  the resolver and transparently forwards everything else to an upstream resolver.
  Run it on `:53` (see [the `:53` port](/guide/running-a-node#the-53-port)) and
  point your OS at it, and `.fn` works everywhere.
- **HTTP API** (default `127.0.0.1:8420`): publish and resolve records, manage
  content, and expose health and peer information. See the [HTTP API
  reference](/guide/http-api).
- **BCH registry integration**: for bare names only, asks the configured
  Electrum/Fulcrum servers which public key currently owns the name. It is not
  involved in self-certifying-name resolution, and record data remains in the
  DHT rather than on-chain.

A **bootstrap** node (`./freedom-names bootstrap`) is a server-mode peer that others
connect to in order to join the network.

## The resolver and cache

Every surface (DNS, HTTP, CLI) funnels through **one** `Resolver`. The resolver
checks a local cache first, then the DHT, caching any hit. The cache is a
100-entry LRU; an entry expires after the smallest record TTL in the set
(5 minutes when none is set), never past the record's signed `eol`, and failed
lookups are not cached. Sharing one resolver keeps behavior identical no matter
how a name is looked up.

For a self-certifying name (`label.<pubKeyID>.fn`), the resolver derives the DHT
key directly from the `<pubKeyID>` suffix. For a bare name (`mysite.fn`), it
routes through the optional name registry to find the owner's public key first
(see below).

## The validator

The heart of the security model is the DHT **validator**. libp2p lets you register
a validator for a key namespace (here, `fn`). Before any node accepts a value for
`/fn/...`, the validator checks:

- the signature is valid for the record's public key,
- the public key hashes to the DHT key it's being stored under (the key→name
  binding),
- the record hasn't expired,
- the resource records are well-formed: a non-empty set of known types only,
  `A`/`AAAA` values that parse as IPv4/IPv6 respectively, a non-empty `CNAME`
  target, a valid `CONTENT` hash.

And when two values compete for the same key, the validator **selects** the
winner: highest sequence number, then latest `eol`, then the larger raw bytes.
Because every node runs this same logic, a forged or stale record can't
propagate.

## The self-certifying / registry seam

Self-certifying names have **no consensus** and no external dependencies.
The registry (globally-unique bare names) is bolted on through a single interface:

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- The resolver sends self-certifying names straight to the DHT.
- For bare names it calls `ResolveOwner`, gets back a **marshaled public key**
  (byte-identical to `FNRecord.PubKey`), derives the DHT key from it, and then
  follows the *exact same* path (fetch → validate → return records).

The BCH-backed registry is wired in whenever an Electrum endpoint is configured
— the default on every known network — so bare-name lookups work out of the box.
On a network with no Electrum servers the registry is simply absent and bare
names resolve to not-found; self-certifying resolution is unaffected either way.
Crucially, **record data never lives on-chain**; only the name→owner binding
does. Read the full design in [Bare names](/guide/bare-names).

## Portability

The node's own libp2p identity (`private.key`) is separate from your **name**
keys, which live under `~/.freedom/keys/`. That means your names are portable: you
can publish them from any node, and moving to a new node doesn't change who owns
what.

## Next

- [**Run Freedom Names**](/guide/running-a-node) and connect it to peers.
- Reference: the [**CLI**](/guide/cli) and the [**HTTP API**](/guide/http-api).
