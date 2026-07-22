# How names work

Freedom Names replaces the registrar with a keypair. This page walks through the
mechanics: what a name is made of, how a record is stored, and why nobody can
overwrite a name they don't own.

## Anatomy of a name

```
mysite . <pubKeyID> . fn
  │          │         │
  │          │         └─ the Freedom Names top-level domain
  │          └─ base36( sha2-256( your marshaled public key ) )
  └─ your human-readable label
```

- **`mysite`** is the label you choose. You can have many labels under one key.
- **`<pubKeyID>`** is derived from your public key; it is *not* chosen. It is the
  lowercase base36 encoding of the SHA-256 multihash of your marshaled Ed25519
  public key.
- **`.fn`** is the top-level domain Freedom Names answers for.

Because `<pubKeyID>` comes *from the key*, the name is **self-certifying**: given
the name, anyone can check that a record was signed by the matching key. There is
no separate directory mapping names to owners to consult (or to attack).

The label may itself contain dots: `blog.mysite.<pubKeyID>.fn` is a subdomain
label under the same key. What routes a name here (rather than to
[bare names](/guide/bare-names)) is the second-to-last label: if it decodes as a base36
sha2-256 multihash it is a `<pubKeyID>`; otherwise the whole name is treated as
a bare name. Bare names follow stricter DNS-label rules: `a-z`, `0-9` and `-`
(not leading or trailing), 1–63 characters, no dots.

## The record

Records for a name are bundled into a single signed structure, the `FNRecord`:

| Field | Meaning |
| --- | --- |
| `label` | the human label, e.g. `mysite` |
| `records` | the resource records (`A` / `AAAA` / `TXT` / `CNAME` / `CONTENT`) |
| `seq` | monotonic sequence number (**higher wins**) |
| `eol` | expiry (unix seconds); the record is invalid after this |
| `pubKey` | the marshaled Ed25519 public key |
| `sig` | Ed25519 signature over a canonical serialization |

The signature covers a **canonical** encoding of the record (fixed field order,
records sorted by type then value) so that signing and verification are stable
regardless of JSON ordering.

## Where records live

Each record is stored in the DHT under a key derived from its own public key:

```
/fn/<pubKeyID>
```

This is the crux of the ownership guarantee: **a record can only live under the
key its public key hashes to.** You cannot publish a record for someone else's
key, because your signature won't match their key, and you cannot publish under
their DHT key, because it's derived from *their* key, not yours.

## Publishing and validation

When you publish, the flow is:

1. The `freedom` CLI signs the record with your private key, filling `pubKey` and
   `sig`.
2. It POSTs the signed JSON to a running node's `/publish` endpoint.
3. The node **verifies** the record before storing it: signature valid, key→name
   binding correct, not expired, records well-formed.
4. The node stores it in the DHT under `/fn/<pubKeyID>`.

Other nodes on the network run the **same validator** whenever they receive the
value, so a forged or unowned record is rejected everywhere, not just at the
publishing node.

Publishing also starts three clocks. The DHT drops stored values after roughly
36 hours, so the publishing node re-puts each of its records every 8 hours to
keep them alive. But it can only re-put the *original signed bytes*; extending
the record's `eol` (7 days from publish) requires the owner's key, so you must
re-run `freedom publish` within 7 days. And if the node goes offline, the record
falls out of the DHT roughly 36 hours after the last re-put, well before the
7-day `eol`, unless another node is still republishing it.

## Resolving

To resolve `mysite.<pubKeyID>.fn`:

1. Parse the name → recover `<pubKeyID>` → derive the DHT key `/fn/<pubKeyID>`.
2. Check the local cache; a hit is returned as-is.
3. On a miss, fetch the signed record from the DHT and return its resource
   records. The resolver does not re-verify the signature itself: validation
   happens at the DHT layer, where every node runs the validator before
   accepting or serving a value.

Resolution is shared by every surface: the DNS server, the HTTP API, and the CLI
all funnel through one resolver.

## Conflict resolution: newest signed wins

Two valid updates to the same name are ordered by `seq`: higher wins, a tie
falls to the later `eol`, and a remaining tie to the larger raw record bytes.
The CLI derives `seq` from wall-clock time on each publish, but always strictly
above the name's current record, so updates keep winning even for two publishes
in the same second or a clock stepped backwards. A later republish supersedes an
earlier one only if it carries a valid signature from the same key: an attacker
can't win the race without the key.

## Why squatting is impossible

There is no global namespace to grab. `mysite.<aliceKey>.fn` and
`mysite.<bobKey>.fn` are simply **different names**, because the key suffix
differs. Alice can't "take" Bob's `mysite`, and neither of them registered
`mysite` in any shared registry. They each just hold a key.

The trade-off is the visible key suffix. Making `mysite.fn` (no suffix) globally
unique is the job of the [name registry](/guide/bare-names), which *does* need consensus and
gets it from an existing blockchain rather than inventing one.

## Next

- See how the pieces fit together in the [**Architecture**](/guide/architecture).
- Or jump in: [**publish your first name**](/guide/your-first-name).
