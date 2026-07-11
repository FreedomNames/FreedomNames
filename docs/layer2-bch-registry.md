# Layer 2: Globally-Unique Names via a Bitcoin Cash Registry

## Why a second layer

Layer 1 (self-sovereign, key-based records) gives every name owner an
unforgeable identity with **no consensus**: a name is `label.<pubKeyID>.fn`,
where `<pubKeyID>` is the base36 sha2-256 hash of the owner's public key. Because
the key *is* the name, squatting is impossible and no ledger is required.

The trade-off is that the human-facing name carries a long key suffix. To offer
clean, globally-unique bare names like `mysite.fn`, we need a way to agree —
across a decentralized network — on **who owns `mysite`**. That requires
consensus. Rather than build our own chain, Layer 2 leans on an existing one:
**Bitcoin Cash (BCH)**, using **CashTokens** (native NFTs) and **CashScript**
covenants. This mirrors how **LBRY** resolves channel/content names: a name maps
to a *controlling claim* selected on-chain, while the claim itself carries a
public key that ties it back to a keypair (Layer 1).

Crucially, the Freedom Names node treats BCH as an **off-chain resolver**: it
*reads* the chain to answer "who owns `mysite`?". Consensus stays entirely on
BCH; Layer 1 remains pure-DHT.

## The seam in code

Layer 1 does not depend on Layer 2. The boundary is one interface
(`registry.go`):

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- `Resolver.dhtKeyForName` (`resolve.go`) sends **self-certifying** names
  straight to the DHT and routes **bare** names through `ResolveOwner` to obtain
  the owner pubkey, then derives the DHT key with `DHTKeyForPubKey`.
- `bchRegistry` (`registry_bch_stub.go`) is the implementation. Today it returns
  `ErrRegistryNotImplemented`; bare-name lookups fall back cleanly and
  self-certifying resolution is unaffected.

`ResolveOwner` returns a **marshaled libp2p public key**, byte-identical to
`FNRecord.PubKey`. So once the owner is known, the rest of resolution (derive DHT
key → fetch signed record → validate → return RR set) is exactly the Layer 1
path. **The record data itself never lives on-chain** — only the name→owner
binding does. This keeps chain writes tiny and record edits free/instant.

## On-chain data model (CashTokens)

Each claimed name is represented by an **NFT** (a CashTokens non-fungible token):

- **Token category**: a single category minted by the registry covenant, so all
  name NFTs share a lineage and can be validated as "genuine registry tokens".
- **NFT commitment** (up to 40 bytes) holds a compact binding. Since 40 bytes is
  tight, the commitment stores:
  - `nameHash` = hash160(normalized name), and
  - `ownerKeyHash` = hash160(marshaled owner pubkey).
  The full normalized name and full owner pubkey are published in an
  **OP_RETURN** output in the same transaction (and/or in
  [BCMR](https://cashtokens.org/docs/bcmr/chip/) metadata), so a resolver can
  recover and verify them against the hashes in the commitment.

A name is **claimed** iff a UTXO exists holding a registry-category NFT whose
`nameHash` matches. Ownership transfer and record-key rotation are just spends of
that UTXO under the covenant rules below.

## Claim / conflict resolution (LBRY-style)

Two people may both try to claim `mysite`. We resolve conflicts the way LBRY's
claimtrie does — by **effective stake**:

- A claim's weight = the BCH amount locked in its claim UTXO plus any *supports*
  (additional UTXOs pledged to it).
- The **controlling claim** for a name is the active claim with the highest
  effective amount at the current chain tip.
- Names are **normalized** before comparison (see below) so `Mysite` and `mysite`
  compete for the same slot.

`ResolveOwner("mysite.fn")` therefore:

1. normalizes `mysite`,
2. asks a BCH indexer for all active claims with that `nameHash`,
3. picks the controlling claim (highest effective amount; deterministic
   tie-break by claim outpoint),
4. recovers the owner pubkey from the OP_RETURN, verifies it against
   `ownerKeyHash` in the NFT commitment, and returns it.

## Covenant rules (CashScript)

A `NameRegistry.cash` covenant enforces, via BCH **native introspection**:

- **Claim**: mint one NFT of the registry category, commitment = `nameHash ||
  ownerKeyHash`, with the accompanying OP_RETURN present and consistent.
- **Support**: attach additional value to an existing claim without changing its
  commitment.
- **Transfer**: spend the claim UTXO to a new one with an updated `ownerKeyHash`,
  authorized by a signature from the *current* owner key. This lets an owner
  rotate the Layer 1 keypair a name points at (e.g. key compromise) while keeping
  the same human name.
- **Renewal / expiry** (optional, LBRY does not expire): encode a
  `heightExpires` and require re-commit before it, so abandoned names free up.

The May 2025 BCH "VM Limits and BigInt" upgrade expands contract compute and
integer precision, which comfortably covers hashing + amount comparisons here.

## Name normalization

To prevent trivial homograph/collision games, normalize before hashing:

1. Unicode NFC, then case-fold (lowercase).
2. Reject or map confusable scripts (initially: restrict to `[a-z0-9-]`, no
   leading/trailing `-`, max length e.g. 63).
3. `nameHash = hash160(normalizedLabel)`.

Normalization must be identical on the client (when claiming) and in every
resolver, so it lives in one shared Go function used by both the covenant-tx
builder and `bchRegistry.ResolveOwner`.

## How each surface uses it

- **DNS server** (`dns.go`) and **HTTP `/resolve`** (`http.go`) already call
  `Resolver.Resolve`, which transparently consults the registry for bare names —
  no changes needed when the stub is replaced with the real resolver.
- **CLI**: a future `freedom claim <name>` / `freedom transfer <name>` will build
  and broadcast the BCH covenant transactions; `freedom lookup mysite.fn` already
  works through the resolver.

## Phase 2 build checklist

1. `NameRegistry.cash` covenant (claim/support/transfer/renew) + tests on BCH
   **testnet4** (chipnet).
2. Shared Go name-normalization function.
3. `bchRegistry.ResolveOwner`: query a BCH indexer (e.g. a Fulcrum/Electrum
   server or a CashTokens-aware indexer) for active claims, select the
   controlling claim, recover + verify the owner pubkey.
4. `freedom claim` / `freedom transfer` CLI commands (needs a BCH wallet;
   CashScript tooling runs under Node.js, invoked out-of-process or via a small
   sidecar).
5. Caching + reorg handling: cache name→owner with a short TTL; invalidate on
   chain reorg past the claim's confirmation depth.
6. Wire `NewBCHRegistry(cfg)` in `main.go` with the indexer endpoint from config.

## Open design questions

- Indexer trust: a light client can be fooled by a lying indexer. Mitigate with
  SPV proofs of the claim UTXO, or by querying multiple indexers.
- Fee/spam economics: minimum stake to claim, and whether supports can be
  withdrawn (LBRY allows withdrawal at any time).
- Interaction with self-certifying names: a bare name always *also* resolves via
  its owner's Layer 1 records, so the two layers compose rather than conflict.
