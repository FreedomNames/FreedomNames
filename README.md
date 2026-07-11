# Freedom Names

Decentralized DNS built on a libp2p Kademlia DHT, written in Go.

Freedom Names lets anyone own a human-readable name and publish DNS-style records
for it, with **no central authority and no consensus** — a name is owned by
whoever holds its Ed25519 keypair. Records are cryptographically signed, so nobody
can overwrite a name they don't own.

## How names work

A name looks like:

```
mysite.<pubKeyID>.fn
```

where `<pubKeyID>` is the base36 hash of the owner's public key (self-certifying,
like IPNS/GNS). Because the key *is* the name, squatting is impossible — no ledger
required. Records are signed `FNRecord`s stored in the DHT under a key derived
from the owner's public key; the validator verifies the signature and the
key→name binding before accepting any update, and the newest record (highest
sequence number) wins.

Globally-unique *bare* names (`mysite.fn`, no key suffix) are a planned **Layer 2**
via an optional Bitcoin Cash registry — see [`docs/layer2-bch-registry.md`](docs/layer2-bch-registry.md).

## Running a node

```sh
go run .
```

A node runs three things at once:

- a **libp2p DHT** peer (the decentralized storage/resolution network),
- a **DNS server** (default `:53`) that resolves `.fn` names and forwards
  everything else upstream — point your OS/browser at it and `.fn` just works,
- an **HTTP API** (default `:8080`) for publishing and resolving.

Run a **bootstrap** (server) node that others can connect to:

```sh
go run . bootstrap
```

### Configuration

All configuration is via environment variables (nothing is hardcoded):

| Variable | Default | Purpose |
|---|---|---|
| `FREEDOM_HTTP_ADDR` | `:8080` | HTTP API listen address |
| `FREEDOM_DNS_ADDR` | `:53` | DNS server listen address |
| `FREEDOM_UPSTREAM_DNS` | `1.1.1.1:53` | Upstream resolver for non-`.fn` queries |
| `FREEDOM_BOOTSTRAP` | (none) | Comma-separated bootstrap peer multiaddrs |

`:53` is privileged. For local development use a high port, e.g.
`FREEDOM_DNS_ADDR=127.0.0.1:15353`, or grant the binary the capability:
`sudo setcap cap_net_bind_service=+ep ./freedom-names`.

## Managing names with the CLI

```sh
# Generate an owner keypair for a name
freedom keygen mysite

# Stage one or more resource records (A | AAAA | TXT | CNAME)
freedom set mysite A 10.0.0.5 300
freedom set mysite TXT "hello world"

# Print your full "mysite.<pubKeyID>.fn" name
freedom name mysite

# Sign the staged records and publish them to a running node
freedom publish mysite --api http://localhost:8080

# Resolve a name via a running node
freedom lookup mysite.<pubKeyID>.fn --type A
```

Keys and staged records live under `~/.freedom/keys/`. The node's own libp2p
identity (`private.key`) is separate, so names are portable between nodes.

Invoke the CLI via the built binary (`./freedom-names freedom keygen mysite`) or,
during development, `go run . freedom keygen mysite`.

## Resolving from your system

Once a node is running, query it like any DNS server:

```sh
dig @127.0.0.1 -p 53 mysite.<pubKeyID>.fn A
```

Non-`.fn` queries are transparently forwarded to the upstream resolver, so the
Freedom Names node can act as your system resolver.

## HTTP API

| Route | Method | Purpose |
|---|---|---|
| `/publish` | POST | Store a signed `FNRecord` (JSON body) |
| `/resolve?name=<name>&type=<TYPE>` | GET | Resolve a name to its records |
| `/peers` | GET | Routing-table peers + connected hosts |
| `/info` | GET | Node mode, peer ID, addresses, network size |
| `/clear_cache` | DELETE | Purge the local resolution cache |

## Development

Run tests (including a live over-the-wire DNS server test):

```sh
go test -race ./...
```

Install [air](https://github.com/air-verse/air) for auto-recompile on changes:

```sh
air
```

## Troubleshooting

To avoid QUIC receive-buffer warnings, increase the kernel limits:

```sh
sudo sysctl -w net.core.rmem_max=7500000
sudo sysctl -w net.core.wmem_max=7500000
```

Make it permanent in `/etc/sysctl.conf`:

```conf
net.core.rmem_max=7500000
net.core.wmem_max=7500000
```
