# FAQ

## Is Freedom Names a blockchain?

No. Self-certifying names have **no consensus at all** and no chain. They don't
need one, because names are derived from public keys and therefore can't
collide. Records live in a peer-to-peer DHT, ordered per-name by a sequence number.

[Bare names](/guide/bare-names) *do* borrow a blockchain (Bitcoin Cash)
to decide who owns a globally-unique name. Resolution only reads the chain
(registering or transferring a bare name writes to it), and none of this makes
self-certifying names a blockchain.

## Can someone squat or steal my name?

No. A self-certifying name is `label.<pubKeyID>.fn`, where `<pubKeyID>` is derived
from *your* public key. Someone else's `mysite` is a **different name** because
their key suffix differs. And nobody can overwrite your records without producing a
valid signature from your key, which they don't have.

## What if I lose my key?

The key **is** the name. If you lose the private key under
`~/.freedom/keys/<label>.key`, you can no longer publish updates for that
self-certifying name. Back it up like you would any critical secret.

[Bare names](/guide/bare-names) add a **transfer** operation that rotates the
keypair a bare name points at (useful after a key compromise) while keeping the
human name; the raw `label.<pubKeyID>.fn` form has no such operation.

## What record types are supported?

`A` (IPv4), `AAAA` (IPv6), `TXT` (any UTF-8 text), `CNAME` (a non-empty
target), and `CONTENT` (a content hash pointing the name at bytes on the
[content network](/guide/content)). The set is intentionally small for now.

## How do updates propagate?

You republish. Each publish carries a sequence number strictly above the name's
current record, and the **newest valid record wins** across the network. Nodes
cache resolutions (a 100-entry LRU; entries live for the smallest record TTL,
5 minutes if none is set, capped at the record's signed expiry; failed lookups
are not cached), so if you want a fresh read immediately after an update you
can clear a node's cache via
[`DELETE /clear_cache`](/guide/http-api#delete-clear_cache).

## Does it break the rest of my DNS?

No. A node resolves `.fn` names itself and **transparently forwards** every other
query to an upstream resolver (`1.1.1.1:53` by default, configurable). So a Freedom
Names node can act as your only resolver. See
[Resolving from your system](/guide/resolving).

## Why the visible key suffix?

Because self-certification requires the name to carry (a hash of) the key. That's
the cost of needing **no registration and no consensus**. Clean bare names like
`mysite.fn` instead pay for global uniqueness with
[on-chain consensus](/guide/bare-names).

## Is it production-ready?

Treat it as early and actively developed. The design is deliberate and the key-layer
guarantees are solid, but this is a young project, so expect sharp edges.

## How is this different from ENS / Handshake / IPNS / GNS?

- **IPNS / GNS**: Freedom Names' key layer uses the same *self-certifying* idea (the
  key hashes into the name). Freedom Names wraps it in **DNS-style records and a DNS
  server**, so `.fn` works with ordinary tooling.
- **ENS / Handshake**: those put the whole namespace on a blockchain. Freedom
  Names keeps self-certifying names **chain-free**, and only reaches for a chain (BCH)
  for [bare names](/guide/bare-names), and even then, only the name→owner
  binding is on-chain, never the records.

## Where's the code?

On GitLab:
[gitlab.melroy.org/freedom-names/freedom-names](https://gitlab.melroy.org/freedom-names/freedom-names).
