# What is Freedom Names?

**Freedom Names is decentralized DNS.** Its self-certifying names need no
central authority or consensus: a name is owned by whoever holds its Ed25519
keypair. Optional bare names use Bitcoin Cash consensus only to establish one
global owner. In both cases records are cryptographically signed, so nobody can
overwrite a name they don't own.

It is a single program written in Go, built on a [libp2p](https://libp2p.io)
Kademlia distributed hash table (DHT).

## The problem it solves

The domain name system we use every day is decentralized in *operation* but
centralized in *control*. Names are leased from registrars, which answer to
registries, which answer to a small set of root operators. That chain of
authority means a name can be:

- **suspended** for a missed payment or a policy dispute,
- **seized** by a court order or a government,
- **hijacked** if a registrar account is compromised.

The site behind the name never changed, yet the name stopped pointing at it.

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

Records (the `A`, `AAAA`, `TXT`, `CNAME`, and `CONTENT` entries a name maps to) are bundled
into a signed `FNRecord` and stored in the DHT under a key derived from your
public key. Every node independently verifies the signature and the key→name
binding before accepting an update, and the newest record (highest sequence
number) wins. For the full mechanics, see [**How names
work**](/guide/how-names-work).

## Two kinds of name

You pick a flavor per name, trading name length against what it costs to own:

- **Self-certifying**: `mysite.<pubKeyID>.fn`. Yours the instant you generate a
  keypair: no registration, no consensus, no coins. The price is the long,
  key-derived suffix.
- **Bare**: `mysite.fn`. Short and suffix-free, but *globally unique*, so the
  network has to agree on who owns `mysite`, and that needs consensus. Freedom
  Names borrows Bitcoin Cash's: claiming a bare name mints a **CashTokens NFT**
  directly on-chain (BCH layer 1, no second layer) that commits to a hash of
  your public key. It's a real NFT you hold in your wallet; resolving one only
  *reads* the chain. Enabled by default.

The two compose: a bare name *also* resolves through its owner's signed DHT
records. See [**bare names**](/guide/bare-names) for the full on-chain protocol.

## What you get

- **Self-sovereign names.** No centralized registrar, no fee, no approval. Generate a key and
  the name is yours.
- **Censorship resistance.** Records live across a peer-to-peer network, not on a
  server anyone can seize.
- **Tamper resistance.** Signed records mean no one can forge or overwrite a name
  they don't hold the key for.
- **Real DNS.** A built-in DNS server resolves `.fn` names and forwards everything
  else upstream for you, so `.fn` "just works" once [your system points at a
  node](/guide/resolving).

## What it is not

- **Not its own blockchain.** Freedom Names invents no chain and mints no coin.
  Self-certifying names have *no consensus at all* and need none; bare names
  reuse Bitcoin Cash's consensus for the single question of *who owns a name*,
  and even then your records live in the DHT, never on-chain.
- **Not production-hardened.** This is an actively developed project. Treat it as
  powerful, promising, and early.

## Try it

Ready to run a node? The [**Quickstart**](/guide/quickstart) gets you a working
local instance in a couple of minutes, then [**Your first
name**](/guide/your-first-name) walks through publishing one.
