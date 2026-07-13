# Layer 2: globally-unique bare names

Layer 1 gives every owner an unforgeable name, but it carries a key suffix
(`mysite.<pubKeyID>.fn`). To offer clean, globally-unique **bare** names like
`mysite.fn`, a decentralized network has to agree on *who owns `mysite`*. That
needs consensus. Rather than build a new chain, Layer 2 leans on an existing
one: **Bitcoin Cash**.

::: info Status
Layer 2 is enabled by default on **mainnet**: a claimed name is a real,
tradeable CashTokens NFT. The node reads the chain through public
Electrum/Fulcrum servers, using a built-in per-network bootstrap list with
automatic failover. To experiment first, switch to a test network with
`FREEDOM_BCH_NETWORK=chipnet` (or `testnet4` / `testnet3`) and use faucet coins.
Self-certifying (Layer 1) names always resolve regardless.
:::

## Why a second layer

Layer 1 (key-based records) has **no consensus** because self-certifying names
cannot collide: `mysite.<aliceKey>.fn` and `mysite.<bobKey>.fn` are just
different names. The trade-off is the visible key suffix.

A bare name has no suffix, so two people *can* both want `mysite`. Deciding who
wins is a consensus problem. Layer 2 borrows Bitcoin Cash's consensus: the
first confirmed claim wins, and the claim is a **CashTokens NFT** you actually
hold in your wallet.

The node treats BCH as an **off-chain resolver**: it *reads* the chain to
answer "who owns `mysite`?". Consensus stays entirely on BCH; Layer 1 stays
pure-DHT.

## The seam in code

Layer 1 does not depend on Layer 2. The whole boundary is one interface:

```go
type NameRegistry interface {
    ResolveOwner(name string) (pubKey []byte, err error)
}
```

- The resolver sends **self-certifying** names straight to the DHT.
- For **bare** names it calls `ResolveOwner` to get the owner's public key,
  derives the DHT key from it, and then follows the *exact same* Layer 1 path.

`ResolveOwner` returns a **marshaled public key**, byte-identical to
`FNRecord.PubKey`. So once the owner is known, resolution (derive DHT key, fetch
signed record, validate, return records) is identical to Layer 1. **The record
data never lives on-chain**; only the name-to-owner binding does. That keeps
chain writes tiny and record edits free and instant.

## The on-chain protocol (FN v1)

A name is a **mutable CashTokens NFT**. Two transaction types anchor it, each
identified by a tag in an `OP_RETURN` output:

**Claim (`FN01`)** registers a name. The transaction:

1. **mints a mutable NFT** whose commitment is `hash160(ownerPubKey)`, sent to
   your own address. This NFT *is* the name deed.
2. carries `OP_RETURN FN01 <name> <ownerPubKey>`, revealing the full Ed25519
   owner key the commitment hashes.
3. pays a small **dust marker** to `hash160("FN:" + name)`, an address nobody
   controls. Every claim/rebind pays this same marker, so all activity for a
   name is discoverable with a single query, and the burned dust is a tiny
   anti-spam registration cost.

**Rebind (`FN02`)** points an existing name NFT at a new owner key. You spend
the NFT, set its commitment to `hash160(newOwnerPubKey)`, and attach
`OP_RETURN FN02 <name> <newOwnerPubKey>`. Because the NFT is a standard token,
you can also **send it in any token-aware wallet**; the new holder then runs
`freedom adopt <name>` once to bind it to their own key.

## Resolution

`ResolveOwner("mysite.fn")`:

1. queries the name's marker script history for claim/rebind transactions,
2. takes the **earliest confirmed** valid `FN01` as authoritative (first come,
   first served). Its transaction id is the NFT category.
3. follows the NFT from its mint output to the current holder and reads the live
   commitment,
4. returns the revealed owner pubkey whose `hash160` matches that commitment
   (falling back to the last valid binding if the NFT was moved by a plain
   wallet transfer that has not been adopted yet).

Results are cached for a few minutes for reorg tolerance.

## Reaching the chain: Electrum servers and failover

A node reads BCH through the **Electrum Cash protocol** (as served by Fulcrum).
It ships with a built-in bootstrap list of public servers **per network** and
tries them in order, **failing over** to the next if one is unreachable, so no
single server is a point of failure. The first server that connects is reused
across reconnects.

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
point at it.
:::

## Name normalization

Names are normalized before use: lowercased, restricted to `[a-z0-9-]` with no
leading or trailing `-` and a length of 1 to 63. The **same** function runs on
the client (when claiming) and in every resolver, so a name means exactly one
thing everywhere.

## How the two layers compose

A bare name always *also* resolves via its owner's Layer 1 records; Layer 2 only
supplies the name-to-owner binding, and Layer 1 supplies the records. The two
layers **compose rather than conflict**: `freedom whois mysite.fn` even prints
the equivalent self-certifying `mysite.<pubKeyID>.fn` name.

## Trying it on chipnet

Bare names default to **mainnet**, where a claim costs real (if tiny) BCH. To
rehearse the whole flow for free, switch to **chipnet** and fund from a faucet.
The built-in chipnet server list is used automatically, so you only set the
network:

```sh
export FREEDOM_BCH_NETWORK=chipnet

freedom keygen mysite            # your Layer 1 owner key
freedom wallet                   # shows a bchtest: address to fund
# fund that address from a chipnet faucet, then:
freedom claim mysite             # mints the name NFT on-chain
freedom whois mysite.fn          # once confirmed, shows the owner
```

For a real, permanent name, drop `FREEDOM_BCH_NETWORK` (mainnet is the default)
and fund the `bitcoincash:` address instead. See the full walkthrough in
[claiming a bare name](/examples/claim-a-bare-name).

## Reserved for later

The design leaves room for a v2 that adds tradeable-name marketplaces and
stake-weighted conflict resolution (LBRY-style). v1 keeps it minimal: a name is
a token, first confirmed claim wins.
