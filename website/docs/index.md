---
layout: home

hero:
  name: Freedom Names
  text: DNS without gatekeepers.
  tagline: >-
    Own a human-readable name with no central authority and no consensus.
    The key <em>is</em> the name — so nobody can take it, squat it, or overwrite it.
  image:
    src: /logo.svg
    alt: Freedom Names
  actions:
    - theme: brand
      text: Get started
      link: /guide/what-is-freedom-names
    - theme: alt
      text: Your first name
      link: /guide/your-first-name
    - theme: alt
      text: View on GitLab
      link: https://gitlab.melroy.org/freedom-names/freedom-names

features:
  - icon: 🔑
    title: The key is the name
    details: >-
      A name is owned by whoever holds its Ed25519 keypair. The public key hashes
      into the name itself (self-certifying, like IPNS/GNS), so ownership needs no
      ledger and squatting is impossible.
  - icon: 🕸️
    title: No central authority
    details: >-
      Records live in a libp2p Kademlia DHT spread across every node. There is no
      registrar to pay, no server to seize, and no single party who can revoke or
      censor a name.
  - icon: ✍️
    title: Cryptographically signed
    details: >-
      Every record is signed by its owner. A validator verifies the signature and
      the key→name binding before accepting any update — nobody can overwrite a
      name they don’t own. Newest signed record wins.
  - icon: 🌐
    title: Just works with DNS
    details: >-
      Point your OS or browser at a Freedom Names node and <code>.fn</code> names
      resolve like any domain. Everything else is transparently forwarded to your
      upstream resolver.
  - icon: 🧰
    title: Batteries included
    details: >-
      One Go binary runs a DHT peer, a DNS server, and an HTTP API at once. A
      <code>freedom</code> CLI generates keys, stages records, and publishes them.
  - icon: 🧭
    title: Clean names, later
    details: >-
      Bare names like <code>mysite.fn</code> are a planned Layer&nbsp;2 over a
      Bitcoin&nbsp;Cash registry — consensus stays on-chain while Layer&nbsp;1
      remains pure DHT.
---

<div class="fn-home-extra">

## Why Freedom Names?

Today, a domain name is a **lease** from a registrar, ultimately backed by a
handful of root operators and registries. Miss a payment, fall foul of a policy,
or land in the wrong jurisdiction, and the name can be suspended, transferred, or
seized — regardless of who actually built the site behind it.

Freedom Names removes the middleman. A name is a **cryptographic fact**, not a
permission someone grants you:

- **You generate a keypair.** The public key hashes into the name, so the name is
  yours the moment the key exists — no registration, no approval, no fee.
- **You sign your records.** IP addresses, text, aliases — all signed by your key
  and stored in a peer-to-peer DHT.
- **Everyone can verify them.** Any node checks the signature and the key→name
  binding independently. There is no authority to trust, only math.

## A name in 60 seconds

```sh
# 1. Generate an owner keypair for a name
freedom keygen mysite

# 2. Point it at a server
freedom set mysite A 10.0.0.5 300

# 3. See your full self-certifying name
freedom name mysite
#   mysite.<pubKeyID>.fn

# 4. Sign the records and publish them to a running node
freedom publish mysite --api http://localhost:8080

# 5. Resolve it — from anywhere on the network
dig @127.0.0.1 -p 53 mysite.<pubKeyID>.fn A
```

That `<pubKeyID>` is the base36 hash of your public key. Because it is *derived
from the key*, the name is self-certifying: whoever can produce a valid signature
for it owns it, full stop.

<div class="fn-cta">

**Ready to try it?** → [Run a node](/guide/running-a-node) ·
[Publish your first name](/guide/your-first-name) ·
[Understand the design](/guide/architecture)

</div>

</div>

<style>
.fn-home-extra {
  max-width: 768px;
  margin: 8px auto 96px;
  padding: 0 24px;
}
.fn-home-extra h2 {
  border-top: 1px solid var(--vp-c-divider);
  padding-top: 32px;
  margin-top: 48px;
}
.fn-cta {
  margin-top: 32px;
  padding: 20px 24px;
  border-radius: 12px;
  background: var(--vp-c-brand-soft);
  border: 1px solid var(--vp-c-divider);
}
</style>
