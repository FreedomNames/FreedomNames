# What is Freedom Names?

**Freedom Names is decentralized DNS.** Its self-certifying names need no
central authority or consensus: a name is owned by whoever holds its Ed25519
keypair. Optional bare names use Bitcoin Cash consensus only to establish one
global owner. In both cases records are cryptographically signed, so nobody can
overwrite a name they don't own.

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

- **Not a blockchain.** Self-certifying names have *no consensus at all*, and need none,
  because self-certifying names can't collide.
- **Not consensus-free for bare names.** `mysite.fn` (no key suffix) requires
  agreeing on *who owns `mysite`*, which does need consensus.
  [bare names](/guide/bare-names) borrow it from a Bitcoin Cash registry, enabled by
  default; unclaimed bare names simply resolve to not-found.
- **Not production-hardened.** This is an actively developed project. Treat it as
  powerful, promising, and early.

## Getting started

Download the prebuilt archive for your operating system and architecture from
the [GitLab releases page](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
(open **Assets → Packages**) or the [GitHub releases
page](https://github.com/FreedomNames/FreedomNames/releases) (open **Assets**).

For example, on 64-bit Linux, extract the downloaded archive and start Freedom
Names (replace `0.8.3` with the version you downloaded):

```sh
tar -xzf freedom-names-0.8.3-linux-amd64.tar.gz
./freedom-names
```

Leave that terminal open. In a second terminal, verify the local API:

```sh
curl http://localhost:8420/info
```

This starts a working local instance. Nearby Freedom Names instances discover
each other over mDNS. Publishing and resolving through the DHT requires at
least one other peer; connecting outside your local network currently requires
a configured bootstrap peer.

For other platforms, peer configuration, and troubleshooting, see [**Run
Freedom Names**](/guide/running-a-node). Once connected, continue with [**Your
first name**](/guide/your-first-name).
