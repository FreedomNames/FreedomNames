---
layout: home

hero:
  name: Freedom Names
  text: DNS without gatekeepers.
  tagline: >-
    Own a self-certifying name with no registry or consensus, or choose a short
    Bitcoin Cash-backed bare name. Your signed records remain yours.
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
      the key→name binding before accepting any update. Nobody can overwrite a
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
      One prebuilt binary runs a DHT peer, content service, DNS server, and HTTP
      API. Its bundled CLI generates keys, stages records, and publishes them.
  - icon: 🧭
    title: Bare names when you want them
    details: >-
      Optional names like <code>mysite.fn</code> use a CashTokens NFT on
      Bitcoin&nbsp;Cash to establish one global owner. Only that short-name
      ownership needs chain consensus; records stay signed and peer-to-peer.
---

<div class="fn-home-extra">

## Why Freedom Names?

Today, a domain name is a **lease** from a registrar, ultimately backed by a
handful of root operators and registries. Miss a payment, fall foul of a policy,
or land in the wrong jurisdiction, and the name can be suspended, transferred, or
seized, regardless of who actually built the site behind it.

Freedom Names removes the registrar. For a self-certifying name, ownership is a
**cryptographic fact**, not permission someone grants you:

- **You generate a keypair.** The public key hashes into the name, so the name is
  yours the moment the key exists: no registration, no approval, no fee.
- **You sign your records.** IP addresses, text, aliases, all signed by your key
  and stored in a peer-to-peer DHT.
- **Everyone can verify them.** Any node checks the signature and the key→name
  binding independently. There is no authority to trust, only math.

If you prefer a short globally unique name such as `mysite.fn`, the optional
Bitcoin Cash registry establishes its owner. The name's DNS and content records
are still signed and distributed through Freedom Names rather than stored on
the chain.

## A name in 60 seconds

With Freedom Names running and connected to at least one peer:

```sh
# 1. Generate an owner keypair for a name
./freedom-names freedom keygen mysite

# 2. Point it at a server
./freedom-names freedom set mysite A 10.0.0.5 300

# 3. See your full self-certifying name
./freedom-names freedom name mysite
#   mysite.<pubKeyID>.fn

# 4. Sign the records and publish them through the running instance
./freedom-names freedom publish mysite --api http://localhost:8420

# 5. Resolve it through your local DNS endpoint
dig @127.0.0.1 -p 8053 mysite.<pubKeyID>.fn A
```

That `<pubKeyID>` is the base36 hash of your public key. Because it is *derived
from the key*, the name is self-certifying: whoever can produce a valid signature
for it owns it, full stop.

<div class="fn-cta">

**Ready to try it?** → [Run Freedom Names](/guide/running-a-node) ·
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
