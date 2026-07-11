# Layer 2: globally-unique bare names

Layer 1 gives every owner an unforgeable name — but it carries a key suffix
(`mysite.<pubKeyID>.fn`). To offer clean, globally-unique **bare** names like
`mysite.fn`, Freedom Names needs a way for a decentralized network to agree on
*who owns `mysite`*. That requires consensus. Rather than build a new chain,
Layer 2 leans on an existing one: **Bitcoin Cash**.

::: info Status
Layer 2 is **planned**. Today the registry is a stub: bare-name lookups return
*not implemented* and fall back cleanly, while self-certifying (Layer 1) names
resolve fully.
:::

## Why a second layer

Layer 1 (key-based records) has **no consensus** because self-certifying names
can't collide — `mysite.<aliceKey>.fn` and `mysite.<bobKey>.fn` are just different
names. The trade-off is the visible key suffix.

A bare name has no suffix, so two people *can* both want `mysite`. Deciding who
wins is a consensus problem. Layer 2 borrows Bitcoin Cash's consensus using
**CashTokens** (native NFTs) and **CashScript** covenants — mirroring how
[LBRY](https://lbry.tech) resolves names via an on-chain *controlling claim*.

The node treats BCH as an **off-chain resolver**: it *reads* the chain to answer
"who owns `mysite`?". Consensus stays entirely on BCH; Layer 1 stays pure-DHT.

## The seam in code

Layer 1 does not depend on Layer 2. The whole boundary is one interface:

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- The resolver sends **self-certifying** names straight to the DHT.
- For **bare** names it calls `ResolveOwner` to get the owner's public key, derives
  the DHT key from it, and then follows the *exact same* Layer 1 path.

`ResolveOwner` returns a **marshaled public key**, byte-identical to
`FNRecord.PubKey`. So once the owner is known, resolution (derive DHT key → fetch
signed record → validate → return records) is identical to Layer 1. **The record
data never lives on-chain** — only the name→owner binding does. That keeps chain
writes tiny and record edits free and instant.

## On-chain data model

Each claimed name is an **NFT** (a CashTokens non-fungible token):

- **Token category** — a single category minted by the registry covenant, so all
  name NFTs share a lineage and can be validated as genuine.
- **NFT commitment** (≤40 bytes) holds a compact binding: `hash160(name)` and
  `hash160(owner pubkey)`. The full name and full owner pubkey go in an
  **OP_RETURN** in the same transaction (and/or in
  [BCMR](https://cashtokens.org/docs/bcmr/chip/) metadata), so a resolver can
  recover and verify them against the hashes.

A name is **claimed** iff a UTXO exists holding a registry-category NFT whose name
hash matches.

## Conflict resolution (LBRY-style)

If two people claim `mysite`, the winner is decided by **effective stake**:

- A claim's weight = the BCH locked in its claim UTXO plus any *supports*.
- The **controlling claim** is the active claim with the highest effective amount
  at the current chain tip (deterministic tie-break by outpoint).
- Names are **normalized** before comparison so `Mysite` and `mysite` compete for
  the same slot.

So `ResolveOwner("mysite.fn")` normalizes the label, asks a BCH indexer for active
claims with that name hash, picks the controlling claim, recovers the owner pubkey
from the OP_RETURN, verifies it against the commitment, and returns it.

## Covenant rules (CashScript)

A `NameRegistry.cash` covenant enforces, via BCH native introspection:

- **Claim** — mint one registry-category NFT with the correct commitment and a
  consistent OP_RETURN.
- **Support** — attach more value to an existing claim without changing it.
- **Transfer** — spend the claim to a new owner-key hash, authorized by the
  *current* owner key. This lets an owner **rotate the Layer 1 keypair** a name
  points at (e.g. after key compromise) while keeping the human name.
- **Renewal / expiry** *(optional)* — encode a `heightExpires` so abandoned names
  free up.

## Name normalization

To prevent homograph/collision games, names are normalized before hashing: Unicode
NFC + case-fold, restricted to `[a-z0-9-]` (no leading/trailing `-`, bounded
length), then `hash160`. Normalization is **identical** on the client and in every
resolver, so it lives in one shared function.

## How the two layers compose

A bare name always *also* resolves via its owner's Layer 1 records — Layer 2 only
supplies the name→owner binding, and Layer 1 supplies the records. The two layers
**compose rather than conflict**.

## Open questions

- **Indexer trust** — a light client can be fooled by a lying indexer. Mitigate
  with SPV proofs of the claim UTXO, or by querying multiple indexers.
- **Fee/spam economics** — minimum stake to claim, and whether supports can be
  withdrawn.

For the full design and the phase-2 build checklist, see
[`docs/layer2-bch-registry.md`](https://gitlab.melroy.org/freedom-names/freedom-names/-/blob/main/docs/layer2-bch-registry.md)
in the repository.
