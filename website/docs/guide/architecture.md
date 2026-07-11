# Architecture

A Freedom Names node is one Go binary that runs three services over a shared
resolver and cache. This page shows how they fit together.

## The three services

Running `go run .` starts all of these at once:

```
                       ┌──────────────────────────────┐
   dig / browser  ───▶ │  DNS server        (:53)     │
                       │  resolves .fn, forwards rest  │
                       └──────────────┬───────────────┘
                                      │
   curl / freedom CLI ──▶ ┌───────────▼──────────────┐
                          │  HTTP API      (:8080)    │
                          │  /publish /resolve /info  │
                          └───────────┬──────────────┘
                                      │
                              ┌───────▼────────┐
                              │    Resolver     │  cache → DHT
                              └───────┬────────┘
                                      │
                       ┌──────────────▼───────────────┐
                       │  libp2p Kademlia DHT peer      │
                       │  stores & serves signed records│
                       └───────────────────────────────┘
```

- **libp2p DHT peer** — the decentralized storage and resolution network. Signed
  records are stored under `/fn/<pubKeyID>` and served to other peers.
- **DNS server** (default `:53`) — resolves `.fn` names through the resolver and
  transparently forwards everything else to an upstream resolver. Point your OS at
  it and `.fn` works everywhere.
- **HTTP API** (default `:8080`) — publish signed records and resolve names
  programmatically. See the [HTTP API reference](/guide/http-api).

A **bootstrap** node (`go run . bootstrap`) is a server-mode peer that others
connect to in order to join the network.

## The resolver and cache

Every surface — DNS, HTTP, CLI — funnels through **one** `Resolver`. The resolver
checks a local cache first, then the DHT, caching any hit. Sharing one resolver
keeps behavior identical no matter how a name is looked up.

For a self-certifying name (`label.<pubKeyID>.fn`), the resolver derives the DHT
key directly from the `<pubKeyID>` suffix. For a bare name (`mysite.fn`), it
routes through the optional Layer 2 registry to find the owner's public key first
— see below.

## The validator

The heart of the security model is the DHT **validator**. libp2p lets you register
a validator for a key namespace (here, `fn`). Before any node accepts a value for
`/fn/...`, the validator checks:

- the signature is valid for the record's public key,
- the public key hashes to the DHT key it's being stored under (the key→name
  binding),
- the record hasn't expired,
- the resource records are well-formed.

And when two values compete for the same key, the validator **selects** the one
with the higher sequence number. Because every node runs this same logic, a forged
or stale record can't propagate.

## The Layer 1 / Layer 2 seam

Layer 1 (self-certifying names) has **no consensus** and no external dependencies.
Layer 2 (globally-unique bare names) is bolted on through a single interface:

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- The resolver sends self-certifying names straight to the DHT.
- For bare names it calls `ResolveOwner`, gets back a **marshaled public key**
  (byte-identical to `FNRecord.PubKey`), derives the DHT key from it, and then
  follows the *exact same* Layer 1 path (fetch → validate → return records).

Today the registry is a stub that returns *not implemented*, so bare-name lookups
fall back cleanly and self-certifying resolution is entirely unaffected. Crucially,
**record data never lives on-chain** — only the name→owner binding does. Read the
full design in [Layer 2](/guide/layer2).

## Portability

The node's own libp2p identity (`private.key`) is separate from your **name**
keys, which live under `~/.freedom/keys/`. That means your names are portable: you
can publish them from any node, and moving to a new node doesn't change who owns
what.

## Next

- [**Run a node**](/guide/running-a-node) and watch it join the network.
- Reference: the [**CLI**](/guide/cli) and the [**HTTP API**](/guide/http-api).
