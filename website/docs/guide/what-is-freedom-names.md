# What is Freedom Names?

**Freedom Names is decentralized DNS.** It lets anyone own a human-readable name
and publish DNS-style records for it, with **no central authority and no
consensus** — a name is owned by whoever holds its Ed25519 keypair. Records are
cryptographically signed, so nobody can overwrite a name they don't own.

It is a single program written in Go, built on a [libp2p](https://libp2p.io)
Kademlia DHT.

## The problem it solves

The domain name system we use every day is decentralized in *operation* but
centralized in *control*. Names are leased from registrars, which answer to
registries, which answer to a small set of root operators. That chain of
authority means a name can be:

- **suspended** for a missed payment or a policy dispute,
- **seized** by a court order or a government,
- **hijacked** if a registrar account is compromised.

The site behind the name never changed — but the name stopped pointing at it.

## The idea

Freedom Names replaces "a name is a lease you're granted" with "a name is a key
you hold."

A name looks like this:

```
mysite.<pubKeyID>.fn
```

`<pubKeyID>` is the base36 hash of the owner's **public key**. Because the key
*is* the name (this is called a *self-certifying* name, the same trick IPNS and
GNU Name System use), there is nothing to register and nothing to squat. If you
can sign for the name, you own it. If you can't, you don't.

Records — the `A`, `AAAA`, `TXT`, and `CNAME` entries a name maps to — are bundled
into a signed `FNRecord` and stored in the DHT under a key derived from your
public key. Every node independently verifies the signature and the key→name
binding before accepting an update, and the newest record (highest sequence
number) wins.

## What you get

- **Self-sovereign names.** No registrar, no fee, no approval. Generate a key and
  the name is yours.
- **Censorship resistance.** Records live across a peer-to-peer network, not on a
  server anyone can seize.
- **Tamper resistance.** Signed records mean no one can forge or overwrite a name
  they don't hold the key for.
- **Real DNS.** A built-in DNS server resolves `.fn` names and forwards everything
  else upstream, so `.fn` "just works" once your system points at a node.

## What it is not (yet)

- **Not a blockchain.** Layer 1 has *no consensus at all* — it doesn't need any,
  because self-certifying names can't collide.
- **Not clean bare names — yet.** `mysite.fn` (no key suffix) requires agreeing on
  *who owns `mysite`*, which does need consensus. That's the planned
  [Layer 2](/guide/layer2) over a Bitcoin Cash registry. Today those names return
  *not implemented* and self-certifying names work fully.
- **Not production-hardened.** This is an actively developed project. Treat it as
  powerful, promising, and early.

## Next steps

<div class="fn-next">

- New here? Read [**How names work**](/guide/how-names-work) for the mechanics.
- Want the big picture? See the [**Architecture**](/guide/architecture).
- Ready to try it? [**Run a node**](/guide/running-a-node) and
  [**publish your first name**](/guide/your-first-name).

</div>

<style>
.fn-next ul { line-height: 1.9; }
</style>
