# Use cases

Freedom Names is a small, self-contained decentralized DNS: self-certifying names
that need no registrar, plus optional short [bare names](/guide/bare-names)
anchored on Bitcoin Cash. Here is what people build with it.

## Censorship-resistant websites

Point a `.fn` name at your server and it keeps resolving no matter what a
registrar, registry, or government decides, because there is no registrar to lean
on in the first place. The name is yours for as long as you hold the key. See
[Host a website on `.fn`](/examples/host-a-website).

## The decentralized web (names *and* content)

A name can carry a `CONTENT` record, so Freedom Names resolves not just an address
but the *page bytes* themselves over its [content network](/guide/content). That
makes one binary a complete backend for a decentralized-web browser: naming and
content together, replacing an IPFS daemon. See [Embedding a
node](/guide/embedding).

## A friendly UI: LibreWeb

Prefer a graphical app over the command line? **[LibreWeb](/guide/libreweb)** is a
sister project: a user-friendly decentralized-web browser that embeds Freedom
Names as its backend, so you get self-sovereign names and peer-to-peer pages
without ever touching a terminal.

## Naming for self-hosted services and homelabs

Give every service on your network a stable, memorable `.fn` name without paying
for a domain or keeping a registrar account. Records are just signed data in the
DHT, so adding or changing a name is instant and free.

## A naming layer for peer-to-peer apps

Developers can spawn `freedom-names` as a child process and drive it over a local
HTTP API, getting self-certifying names and content transport without building
either from scratch. See [Embedding a node](/guide/embedding).

## Verifiable keys and proofs

Publish public keys, domain-verification tokens, or contact details under a `.fn`
name as `TXT` records that anyone can verify and nobody can revoke. Because the
name is a keypair you hold rather than a lease you rent, the proof stays valid
wherever you host it. See [Publish a TXT record](/examples/txt-record).

## Next

- Learn the mechanics in [**How names work**](/guide/how-names-work).
- Or jump in with the [**Quickstart**](/guide/quickstart).
