# Bare names

A **bare name** is a short, fully human-readable name like `mysite.fn` — no key
suffix, globally unique. Because there is no suffix to tell two claimants apart,
a decentralized network has to agree on *who owns `mysite`*. That needs
consensus. Rather than build a new chain, Freedom Names leans on an existing
one: **Bitcoin Cash**.

::: info Both mechanisms give you a name
Self-certifying names already contain a human-readable label you choose —
`mysite.<pubKeyID>.fn` is yours the moment you generate a keypair, with no
registry, no consensus, and no coins. The catch is the key-derived suffix, which
makes the name long and awkward to share.

The name registry exists to offer the short version (`mysite.fn`). That
shortness is exactly what requires consensus, which is why bare names need
Bitcoin Cash and [self-certifying names](/guide/how-names-work) do not.
:::

::: warning Not a blockchain "layer 2"
Freedom Names is **not** built on top of Bitcoin Cash the way a rollup or a
payment channel is. BCH scales on-chain by design and CashTokens are native
on-chain primitives — no second layer required.

Nothing is settled off-chain: **resolving** a bare name only *reads* the chain,
and registering or transferring one writes to it directly — `freedom claim` and
`freedom adopt` broadcast ordinary BCH transactions (see [the on-chain
protocol](#the-on-chain-protocol-fn-v1) below).
:::

::: info Status
Bare names are enabled by default on **mainnet**: a claimed name is a real,
tradeable CashTokens NFT. The node reads the chain through public
Electrum/Fulcrum servers, using a built-in per-network bootstrap list with
automatic failover. To experiment first, switch to a test network with
`FREEDOM_BCH_NETWORK=chipnet` (or `testnet4` / `testnet3`) and use faucet coins.
Self-certifying names always resolve regardless.
:::

## Why a name registry

Self-certifying names (key-based records) need **no consensus** because they
cannot collide: `mysite.<aliceKey>.fn` and `mysite.<bobKey>.fn` are simply
different names. The trade-off is the visible key suffix.

A bare name has no suffix, so two people *can* both want `mysite`. Deciding who
wins is a consensus problem. The registry borrows Bitcoin Cash's consensus: the
first confirmed claim wins, and the claim is a **CashTokens NFT** you actually
hold in your wallet.

The node treats BCH as an **off-chain resolver**: it *reads* the chain to
answer "who owns `mysite`?". Consensus stays entirely on BCH; the key layer
stays pure-DHT.

## The seam in code

The key layer does not depend on the registry. The whole boundary is one
interface:

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- The resolver sends **self-certifying** names straight to the DHT.
- For **bare** names it calls `ResolveOwner` to get the owner's public key,
  derives the DHT key from it, and then follows the *exact same* path as a
  self-certifying name.

`ResolveOwner` returns a **marshaled public key**, byte-identical to
`FNRecord.PubKey`. So once the owner is known, resolution (derive DHT key, fetch
signed record, validate, return records) is identical to a self-certifying
name. **The record data never lives on-chain**; only the name-to-owner binding
does. That keeps chain writes tiny and record edits free and instant.

## The on-chain protocol (FN v1)

A name is a **mutable CashTokens NFT**. Two transaction types anchor it, each
identified by a tag in an `OP_RETURN` output:

**Claim (`FN01`)** registers a name. The transaction:

1. **mints a mutable NFT** whose commitment is `hash160(ownerPubKey)`, sent to
   your own address as a 1000-sat output. This NFT *is* the name deed. The mint
   must spend a coin at output index 0 (the CashTokens genesis rule — see the
   [walkthrough](/examples/claim-a-bare-name) if `claim` reports "no eligible
   genesis UTXO").
2. carries `OP_RETURN FN01 <name> <ownerPubKey>`, revealing the full Ed25519
   owner key the commitment hashes.
3. pays a small **dust marker** (546 sat) to a standard P2PKH address whose
   pubkey hash is `hash160("FN:" + name)` — a hash with no known key, so the
   dust is effectively unspendable. Every claim/rebind pays this same marker, so
   all activity for a name is discoverable with a single query, and the locked
   dust is a tiny anti-spam registration cost.

**Rebind (`FN02`)** points an existing name NFT at a new owner key. You spend
the NFT, set its commitment to `hash160(newOwnerPubKey)`, and attach
`OP_RETURN FN02 <name> <newOwnerPubKey>`. Because the NFT is a standard token,
you can also **send it in any token-aware wallet**; the new holder then runs
`freedom adopt <name>` once to bind it to their own key.

A claim is **permanent**: names never expire, and there is no renewal or
revocation mechanism. Ownership only changes when the NFT itself moves.

## Resolution

`ResolveOwner("mysite.fn")`:

1. queries the name's marker script history for claim/rebind transactions,
2. takes the **earliest confirmed** valid `FN01` as authoritative (first come,
   first served; two claims in the same block are broken deterministically by
   the numerically smaller transaction id, so every resolver agrees). Its
   transaction id is the NFT category.
3. follows the NFT from its mint output to the current holder and reads the live
   commitment,
4. returns the revealed owner pubkey whose `hash160` matches that commitment.

Only transactions with at least `FREEDOM_BCH_MINCONF` confirmations count; the
setting is floored to 1, so unconfirmed transactions never do.

A few consequences of the commitment-match rule:

- A **plain wallet transfer** leaves the commitment unchanged, so the name keeps
  resolving to the previous owner's key until the new holder runs
  `freedom adopt`. If the live commitment matches **no** revealed key, the name
  simply has no resolvable owner — there is no fallback binding.
- That absence of a fallback is what makes hijacking impossible: a pubkey only
  becomes authoritative by matching the live NFT commitment, so a stranger who
  merely pays the marker dust and posts an `FN02` without holding the NFT can
  never take over the name.
- If the NFT is **burned** (spent into a transaction with no output carrying the
  token), the last commitment before the burn stands, so the name keeps
  resolving to its pre-burn owner.

Successful lookups are cached for 5 minutes for reorg tolerance; a not-found
answer is cached for only 30 seconds, so a freshly confirmed claim becomes
visible quickly.

## Reaching the chain: Electrum servers and failover

A node reads BCH through the **Electrum Cash protocol** (as served by Fulcrum).
It ships with a built-in bootstrap list of public servers **per network** and
tries them in order, **failing over** to the next if one is unreachable, so no
single server is a point of failure. The first server that connects is reused
across reconnects.

If **every** server is unreachable, bare-name resolution fails with a transient
error. Nothing is cached in that case, so the next lookup retries the full
list; `freedom wallet` still prints your address but shows the balance as
unavailable. Self-certifying names are unaffected.

Two things you can override:

- `FREEDOM_BCH_NETWORK` selects the chain (`mainnet` by default; `chipnet`,
  `testnet4`, or `testnet3` for testing). Each network has its own bootstrap
  list and address prefix.
- `FREEDOM_BCH_ELECTRUM` replaces the bootstrap list with your own
  comma-separated servers (`ssl://host:port`, tried in order with the same
  failover).

::: warning Privacy
Any public Electrum server sees which bare names you resolve. For privacy or
guaranteed availability, run your own Fulcrum and set `FREEDOM_BCH_ELECTRUM` to
point at it. It must speak Electrum protocol **1.5 or newer** — the version
that added CashTokens data, which claims and resolution depend on.
:::

## Name normalization

Names are normalized before use: lowercased, restricted to `[a-z0-9-]` with no
leading or trailing `-` and a length of 1 to 63. A bare name cannot contain
dots, and anything outside that charset — including non-ASCII names — is
rejected outright; there is no punycode/IDNA mapping for Unicode names. The
**same** function runs on the client (when claiming) and in every resolver, so
a name means exactly one thing everywhere.

## How the two compose

A bare name always *also* resolves via its owner's signed DHT records; the registry only
supplies the name-to-owner binding, and the DHT supplies the records. The two
mechanisms **compose rather than conflict**: `freedom whois mysite.fn` even prints
the equivalent self-certifying `mysite.<pubKeyID>.fn` name.

## Trying it on chipnet

Bare names default to **mainnet**, where a claim costs real (if tiny) BCH. To
rehearse the whole flow for free, switch to **chipnet** and fund from a faucet.
The built-in chipnet server list is used automatically, so you only set the
network:

```sh
export FREEDOM_BCH_NETWORK=chipnet

./freedom-names freedom keygen mysite  # your owner key
./freedom-names freedom wallet         # shows a bchtest: address to fund
# fund that address from a chipnet faucet, then:
./freedom-names freedom claim mysite   # mints the name NFT on-chain
./freedom-names freedom whois mysite.fn  # once confirmed, shows the owner
```

For a real, permanent name, drop `FREEDOM_BCH_NETWORK` (mainnet is the default)
and fund the `bitcoincash:` address instead. See the full walkthrough in
[claiming a bare name](/examples/claim-a-bare-name).

## Reserved for later

The design leaves room for a v2 that adds tradeable-name marketplaces and
stake-weighted conflict resolution (LBRY-style). v1 keeps it minimal: a name is
a token, first confirmed claim wins.
