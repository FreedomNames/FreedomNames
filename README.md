# Freedom Names

Freedom Names lets you publish and resolve `.fn` names without a central DNS
provider. One node also gives you a local DNS resolver, HTTP API, and
peer-to-peer content service.

📖 **Full documentation: [freedomnames.org](https://freedomnames.org)**. Guides,
CLI and HTTP API reference, and worked examples.

There are two kinds of name:

- **Self-certifying names**, such as `blog.<pubKeyID>.fn`, are owned by the
  person with the matching key.
- **Bare names**, such as `blog.fn`, are short globally-unique names backed by
  Bitcoin Cash.

For the technical model, see [How Freedom Names work](HOW_FREEDOM_NAMES_WORK.md).

## Install a bootstrap node

For a public Debian or Ubuntu server that helps other nodes join, run our
`scripts/install.sh` bootstrap installer. From a checkout:

```sh
sudo ./scripts/install.sh bootstrap
```

Or run that same script directly from the repository:

```sh
curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | \
  sudo bash -s -- bootstrap
```

It detects the supported Linux architecture, downloads the latest release and
its `SHA256SUMS`, verifies the archive, installs `freedom-names` to
`/usr/local/bin`, and installs/enables/starts
`freedom-names-bootstrap`. The service runs as the dedicated `freedom` user;
its durable peer identity is `/home/freedom/.freedom/private.key`.

This installer is intentionally for the long-running bootstrap-server profile
only. For a normal foreground node, see the [two-path Quickstart](https://freedomnames.org/guide/quickstart).

### Manual bootstrap installation

If you prefer not to run the installer, download, verify, and unpack the
release yourself. Replace `0.9.5` and `amd64` for the release and architecture
you choose:

```sh
VERSION=0.9.5
ARCH=amd64
curl -fLO "https://github.com/FreedomNames/FreedomNames/releases/download/${VERSION}/freedom-names-${VERSION}-linux-${ARCH}.tar.gz"
curl -fLO "https://github.com/FreedomNames/FreedomNames/releases/download/${VERSION}/SHA256SUMS"
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "freedom-names-${VERSION}-linux-${ARCH}.tar.gz"
```

Install the unpacked binary yourself:

```sh
getent group freedom >/dev/null || sudo groupadd --system freedom
id freedom >/dev/null 2>&1 || sudo useradd --system --gid freedom \
  --create-home --home-dir /home/freedom --shell /usr/sbin/nologin freedom
sudo install -m 755 freedom-names /usr/local/bin/freedom-names
```

Then follow the [manual bootstrap-service steps](https://freedomnames.org/examples/bootstrap-node#manual-installation)
to inspect and copy the exact systemd unit, enable it, open the required p2p
ports, and back up the identity key.

## Run a normal node manually

Download the prebuilt archive for your operating system and architecture from
[GitLab Releases](https://gitlab.melroy.org/freedom-names/freedom-names/-/releases)
(**Assets → Packages**) or [GitHub
Releases](https://github.com/FreedomNames/FreedomNames/releases) (**Assets**).

For example, the 64-bit Linux package extracts to the executable and the
bootstrap systemd unit used by the installer. Replace
`0.9.5` with the version you downloaded:

```sh
tar -xzf freedom-names-0.9.5-linux-amd64.tar.gz
./freedom-names
```

Each release also ships a `SHA256SUMS` file covering every archive, so a
download can be checked against it before you run it:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

Choose `linux-arm64` for 64-bit ARM Linux, `darwin-amd64` for an Intel Mac,
`darwin-arm64` for Apple Silicon, or the matching Windows `.zip`. On Windows,
run `.\freedom-names.exe` in PowerShell.

A node runs several things at once:

- a **libp2p DHT** peer (the decentralized naming/discovery network),
- a **content network** that stores and serves page bytes peer-to-peer (a
  content-addressed blobstore + a stream protocol), so a name can point at an
  actual page, not just DNS records. This is what lets Freedom Names back a
  decentralized-web browser such as LibreWeb, replacing IPFS,
- a **DNS server** (default `:8053`) that resolves `.fn` names for anyone and
  forwards everything else upstream for local clients. Point your OS/browser at
  it (or bridge it to `:53`, see below) and `.fn` just works,
- an **HTTP API** (default `127.0.0.1:8420`) for publishing, resolving, and
  content.

Nearby nodes discover each other with mDNS. Beyond the local network, a node
dials the built-in default bootstrap peers; set `FREEDOM_BOOTSTRAP` to use your
own instead.

### Configuration

All configuration is via environment variables (nothing is hardcoded):

| Variable | Default | Purpose |
|---|---|---|
| `FREEDOM_HTTP_ADDR` | `127.0.0.1:8420` (bootstrap: `127.0.0.1:8430`) | HTTP API listen address (loopback by default) |
| `FREEDOM_AUTHORING_ADDR` | `127.0.0.1:8421` | Owner-key API; non-loopback values are refused |
| `FREEDOM_DNS_ADDR` | `:8053` | DNS server listen address |
| `FREEDOM_UPSTREAM_DNS` | `1.1.1.1:53` | Upstream resolver for non-`.fn` queries |
| `FREEDOM_DNS_RECURSION` | `local` | Who may have non-`.fn` queries forwarded upstream. `local` serves this machine and the local network; `any` makes the node a public open resolver (see below) |
| `FREEDOM_HTTP_ALLOWED_HOSTS` | (none) | Extra `Host` header values the HTTP API accepts, beyond `localhost` and IP literals |
| `FREEDOM_BOOTSTRAP` | (built-in list) | Comma-separated bootstrap peer multiaddrs. Overrides the built-in defaults |
| `FREEDOM_CONTENT_DIR` | `~/.freedom/content` | On-disk directory for the content-addressed blobstore |
| `FREEDOM_BCH_NETWORK` | `mainnet` | BCH network for bare names: `mainnet`, `chipnet`, `testnet4`, or `testnet3` |
| `FREEDOM_BCH_ELECTRUM` | (built-in list per network) | Comma-separated Electrum/Fulcrum servers, tried in order with failover (`ssl://` or `tcp://`). Overrides the built-in Electrum list |
| `FREEDOM_BCH_MINCONF` | `1` | Confirmations required before a bare-name claim counts |
| `FREEDOM_CONTENT_REPLICAS` | `3` | Copies pushed to other nodes per publish |
| `FREEDOM_CONTENT_HOST_BUDGET` | `20G` | Maximum hosted content from other publishers |
| `FREEDOM_CONTENT_HOST_TTL` | `30d` | Hosted-content eviction protection after last access or push |
| `FREEDOM_CONTENT_HEAL_INTERVAL` | `1h` | How often holders restore missing replicas |
| `FREEDOM_CONTENT_UP_RATE` | `0` | Upload limit in bytes/s (`0` is unlimited) |
| `FREEDOM_CONTENT_DOWN_RATE` | `0` | Download limit in bytes/s (`0` is unlimited) |
| `FREEDOM_CONTENT_MAX_PUSH_SIZE` | `1G` | Largest content set accepted through replica push |

The DNS server defaults to the high port **`:8053`** so a node runs **without
root**. If the DNS port fails to bind, the node logs a warning and keeps
running; the DHT and HTTP API are unaffected.

For **system-wide** resolution your OS/browser needs Freedom Names on the
standard `:53`. Options:

- Grant the binary the capability once (recommended):
  `sudo setcap cap_net_bind_service=+ep ./freedom-names`, then run with
  `FREEDOM_DNS_ADDR=:53`.
- Or keep `:8053` and forward `:53 → :8053` with a local resolver
  (dnsmasq/systemd-resolved), or point a stub resolver at `127.0.0.1:8053`.

`.fn` names are answered for anyone who can reach the listen address — that is
public, authoritative data. Everything *else* is only forwarded upstream for
clients on this machine or the local network, so a node on a public IP is not an
open resolver that strangers can bounce traffic off. Set
`FREEDOM_DNS_RECURSION=any` only if you intend to run a public forwarder.

The HTTP API is unauthenticated and bound to loopback. It additionally rejects
requests whose `Host` header is a domain name (which is how a DNS-rebinding
attack reaches a local service) and mutating requests carrying a foreign
`Origin` (cross-site request forgery from a page you merely visited). Command
line clients send neither header and are unaffected; if you reach the API
through a hostname, list it in `FREEDOM_HTTP_ALLOWED_HOSTS`.

## Managing names with the CLI

### Self-certifying names

```sh
# Generate an owner keypair for a name
./freedom-names freedom keygen mysite

# Stage one or more resource records (A | AAAA | TXT | CNAME | CONTENT)
./freedom-names freedom set mysite A 10.0.0.5 300
./freedom-names freedom set mysite TXT "hello world"

# Print your full "mysite.<pubKeyID>.fn" name
./freedom-names freedom name mysite

# Sign the staged records and publish them to a running node
./freedom-names freedom publish mysite --api http://localhost:8420

# Resolve a name via a running node
./freedom-names freedom lookup mysite.<pubKeyID>.fn --type A
```

Keys and staged records live under `~/.freedom/keys/`. The node's own libp2p
identity (`~/.freedom/private.key`) is separate, so names are portable between
nodes. (A `private.key` already sitting in the working directory is still used,
so an existing node keeps its peer id.)

Records are bounded, because every node in the DHT neighbourhood carries them:
at most **32 resource records** per name, a **`TXT` value of 255 bytes** (the
DNS character-string limit), a **`CNAME` target of 253**, and a **label of
190**. `set` and `publish` reject anything larger.

### Bare names on Bitcoin Cash

These commands talk **directly to an Electrum server** (no running node needed);
they operate on a single-key BCH wallet at `~/.freedom/bch.key`. They default to
mainnet; prefix with `FREEDOM_BCH_NETWORK=chipnet` to practise with faucet coins.

```sh
# Show your BCH funding address, balance, and claimed-name (NFT) count
./freedom-names freedom wallet

# Claim a globally-unique bare name (mints the FN01 NFT, binds it to your key)
./freedom-names freedom claim mysite

# Re-bind a name NFT you already hold to your current key (e.g. after a
# plain wallet transfer moved it) so it resolves again
./freedom-names freedom adopt mysite

# Look up the on-chain owner of a bare name and its equivalent self-certifying name
./freedom-names freedom whois mysite.fn
```

Once claimed, `mysite.fn` resolves through the same node/DNS/HTTP paths as a
self-certifying name; the node reads the owner straight from the BCH chain.

**Privacy note:** any public Electrum server sees which bare names you resolve.
For privacy (or guaranteed availability) run your own Fulcrum and point the node
at it, which also overrides the built-in Electrum list:

```sh
FREEDOM_BCH_ELECTRUM=ssl://your-fulcrum.example:50002 ./freedom-names
```

Every CLI command is invoked as `./freedom-names freedom <command>` through the
same downloaded binary that runs the node.

## Resolving from your system

Once a node is running, query it like any DNS server:

```sh
dig @127.0.0.1 -p 8053 mysite.<pubKeyID>.fn A
```

Non-`.fn` queries are transparently forwarded to the upstream resolver, so the
Freedom Names node can act as your system resolver. Forwarding is done for
clients on this machine and the local network; remote clients get `REFUSED`
(see [Configuration](#configuration)), while `.fn` is answered for everyone.

## HTTP API

| Route | Method | Purpose |
|---|---|---|
| `/publish` | POST | Store a signed `FNRecord` (JSON body) |
| `/resolve?name=<name>&type=<TYPE>` | GET | Resolve a name to its records |
| `/record?name=<name>` | GET | Fetch the raw signed record (includes seq and expiry) |
| `/content` | POST/GET | Store page bytes (`POST`) or fetch by `?hash=` (`GET`) |
| `/resolve-content?name=<name>` | GET | Resolve a name to its `CONTENT` bytes in one call |
| `/peers` | GET | Routing-table peers + connected hosts |
| `/info` | GET | Version, mode, peer ID, addresses, network size |
| `/health` | GET | Liveness + version handshake |
| `/clear_cache` | DELETE | Purge the local resolution cache |
| `:8421/authoring/names` | GET/POST | List or create locally owned names (separate loopback origin) |
| `:8421/authoring/names/<label>/publish` | POST | Build, sign and publish records (separate loopback origin) |

Content responses (`/content` GET and `/resolve-content`) carry a `Content-Type`
header sniffed from the first bytes (e.g. `image/png`, `text/plain`), since the
content-addressed store keeps no MIME metadata. Unrecognized bytes fall back to
`application/octet-stream`.

## Project structure

```
cmd/freedom-names/   the binary's entry point — wiring only
internal/            all implementation code, private to this module
  version/           build-stamped version string
  config/            FREEDOM_* environment configuration
  record/            signed name records: types, signing, validation
  content/           content-addressed blob store, hosting index, rate limits
  registry/          bare-name rules and the name-registry interface
  bch/               Bitcoin Cash: transactions, wallet, Electrum, registry
  node/              libp2p host, Kademlia DHT, content transfer + replication
  resolver/          name -> records resolution and caching
  dnsserver/         the .fn DNS server
  httpapi/           the local HTTP API
  authoring/         owner-key management and signed-record construction
  cli/               the `freedom-names freedom` subcommands
  bind/              listener bind-error classification
  testsupport/       fixtures shared by more than one package's tests
scripts/             build, format, test and network-verification scripts
assets/              logo and repository images
website/             the VitePress documentation site
```

Everything lives under `internal/`, so the compiler enforces that none of it is
an importable public API. Dependencies flow one way from the entry point through
explicit package boundaries. `content` sits near the bottom; `record` reuses its
content-hash validation, and `config` reuses its content-size limit.

Two interfaces are deliberately declared by their *consumer* rather than their
implementer — `httpapi.FreedomDHT` and `resolver.RecordStore`. Go satisfies
interfaces structurally, so `node` implements both without those interfaces
depending on its concrete type. `resolver` therefore avoids importing `node`;
`httpapi` imports it separately for the content service.

Tests live beside the code they cover, generally in the same package so they can
exercise unexported implementation details; there is no separate test tree.
Dependencies are managed by `go.mod`; there is no vendor directory.

## Development

Developers working from a source checkout can run the tests (including a live
over-the-wire DNS server test):

```sh
go test -race ./...
```

Build a development binary (stamps the version from the nearest git tag):

```sh
./scripts/build.sh
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
