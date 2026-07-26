# Configuration

All configuration is via **environment variables**; nothing is hardcoded, so a
node is entirely driven by its environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `FREEDOM_HTTP_ADDR` | `127.0.0.1:8420` (bootstrap: `127.0.0.1:8430`) | HTTP API listen address (loopback by default) |
| `FREEDOM_DNS_ADDR` | `:8053` | DNS server listen address |
| `FREEDOM_UPSTREAM_DNS` | `1.1.1.1:53` | Upstream resolver for non-`.fn` queries |
| `FREEDOM_DNS_RECURSION` | `local` | Who may have non-`.fn` queries forwarded upstream. `local` = this machine and the local network; `any` = a public open resolver |
| `FREEDOM_HTTP_ALLOWED_HOSTS` | *(none)* | Extra `Host` header values the HTTP API accepts, beyond `localhost` and IP literals |
| `FREEDOM_CONTENT_DIR` | `~/.freedom/content` | Content-addressed blobstore directory |
| `FREEDOM_BOOTSTRAP` | *(built-in list)* | Comma-separated bootstrap peer multiaddrs. Overrides the built-in defaults |
| `FREEDOM_BCH_NETWORK` | `mainnet` | BCH network for bare names: `mainnet`, `chipnet`, `testnet4`, or `testnet3` |
| `FREEDOM_BCH_ELECTRUM` | *(built-in list per network)* | Comma-separated Electrum/Fulcrum servers, tried in order with failover (`ssl://host:port`). Overrides the built-in bootstrap list |
| `FREEDOM_BCH_MINCONF` | `1` | Confirmations before a name claim counts |
| `FREEDOM_CONTENT_REPLICAS` | `3` | Copies pushed to other nodes per publish (target holders = this + 1) |
| `FREEDOM_CONTENT_HOST_BUDGET` | `20G` (20 GiB) | Max bytes of hosted (other people's) content |
| `FREEDOM_CONTENT_HOST_TTL` | `30d` | Hosted content loses eviction protection this long after last access/re-push |
| `FREEDOM_CONTENT_HEAL_INTERVAL` | `1h` | How often each holder checks and tops up replica counts |
| `FREEDOM_CONTENT_UP_RATE` | `0` (unlimited) | Bytes/s cap on serving + pushing content |
| `FREEDOM_CONTENT_DOWN_RATE` | `0` (unlimited) | Bytes/s cap on fetching + receiving pushes |
| `FREEDOM_CONTENT_MAX_PUSH_SIZE` | `1G` (1 GiB) | Largest pushed content set this node accepts |

Size values (`…_BUDGET`, `…_RATE`, `…_MAX_PUSH_SIZE`) take a plain byte count or
a `K`/`M`/`G`/`T` suffix (1024-based; an optional `B`/`iB` is accepted, so `20G`,
`20GB` and `20GiB` all mean 20 GiB). Duration values (`…_TTL`, `…_INTERVAL`)
use Go duration syntax (`1h`, `90m`) plus a `d` suffix for days (`30d`). The
`FREEDOM_CONTENT_*` replication knobs are explained in
[the content network](/guide/content#replication-distributed-by-design).

The HTTP API binds to **`127.0.0.1`** by default: it is an unauthenticated local
control surface (a browser or app spawns the node), so it must not be exposed on
all interfaces. Set `FREEDOM_HTTP_ADDR=:8420` to share it on a LAN deliberately.

Because it is unauthenticated, the API also defends itself against being driven
by a web page you merely visited:

- Requests whose `Host` header is a **domain name** are refused with `403`. That
  is how a DNS-rebinding attack reaches a service on `localhost`. `localhost`
  and IP literals (`127.0.0.1`, `[::1]`, a LAN address) are accepted; if you
  reach the API through a real hostname, list it in
  `FREEDOM_HTTP_ALLOWED_HOSTS` (a port on the entry is fine — it is ignored when
  matching).
- Requests a browser reports as coming from another site
  (`Sec-Fetch-Site: cross-site`) are refused with `403`, whatever the method.
  This one matters for reads too: `GET /content` fetches from the network on a
  miss and keeps what it fetched, announcing this node as a provider — so
  without it, a page you visited could pick what your node hosts and advertises
  just by embedding an `<img>`. Such a request carries no `Origin` at all, which
  is why the `Origin` check below cannot cover it.
- Requests carrying an `Origin` header from another site are refused with `403`
  — cross-site request forgery.

`curl`, the CLI and an embedding app send none of these headers and are
unaffected, as are a URL you typed yourself and a page served from this node.

A spawning host can also override these with **flags**, which take precedence
over the environment: `--http-addr HOST:PORT`, `--api-bind HOST`,
`--content-dir DIR`, `--dns-addr HOST:PORT`. Note that `--api-bind` replaces
only the bind host and keeps the port of the current HTTP address (the
`FREEDOM_HTTP_ADDR`/`--http-addr` value, `8420` if that has no port). See
[embedding a node](/guide/embedding).

The `FREEDOM_BCH_*` variables drive [bare names](/guide/bare-names)
(globally-unique names on Bitcoin Cash), which are on by default on **mainnet**. The node
reaches the chain through a built-in per-network list of public Electrum Cash
(Fulcrum) servers, trying them in order with **failover**. Set
`FREEDOM_BCH_NETWORK` to a test network (`chipnet`, `testnet4`, `testnet3`) to
experiment with faucet coins, or `FREEDOM_BCH_ELECTRUM` to a comma-separated list
of your own servers. Self-certifying names always resolve regardless of these settings.

The DNS server defaults to the high port **`:8053`**, so a node runs **without
root**. If the DNS port can't be bound, the node logs a warning and keeps running;
the DHT and HTTP API are unaffected.

### Who the DNS server answers

The DNS listen address covers **every interface** by default, so it is worth
being precise about what a stranger who reaches it can ask for:

- **`.fn` queries: answered for anyone.** These are authoritative, public data —
  the same answer everyone gets, straight from the DHT.
- **Everything else: forwarded only for local clients** (loopback, and private
  or link-local addresses). A node that forwarded arbitrary queries for the
  whole internet would be an **open resolver**: a tool strangers can bounce
  amplified traffic off at a third party. Remote clients get `REFUSED`.

Every setup in these docs points a resolver at `127.0.0.1`, so this costs you
nothing. If you deliberately want a public forwarder, opt in:

```sh
FREEDOM_DNS_RECURSION=any ./freedom-names
```

The node logs a warning at startup when you do. Only run that on a resolver you
intend to expose, with your own rate limiting in front of it.

## Examples

**Default local start** works on `:8053`, with no `sudo` needed:

```sh
FREEDOM_HTTP_ADDR=127.0.0.1:8420 \
./freedom-names
```

**Different upstream resolver**, forwarding non-`.fn` queries elsewhere:

```sh
FREEDOM_UPSTREAM_DNS=9.9.9.9:53 ./freedom-names
```

**Join a network via bootstrap peers**:

```sh
FREEDOM_BOOTSTRAP="/ip4/<PUBLIC_IP>/tcp/4020/p2p/<peerID>,/ip4/…/…" ./freedom-names
```

## System-wide resolution and the `:53` port

The default `:8053` is great for testing (`dig -p 8053 …`), but your OS and
browser only send DNS to the standard **`:53`**. To make `.fn` resolve
system-wide, run Freedom Names on `:53`. Since `:53` is privileged, either grant
the binary the capability once:

```sh
sudo setcap cap_net_bind_service=+ep ./freedom-names
FREEDOM_DNS_ADDR=:53 ./freedom-names
```

…or keep `:8053` and forward `:53 → 127.0.0.1:8053` with a local resolver
(dnsmasq / systemd-resolved), or point a stub resolver at `127.0.0.1:8053`.

> **Avoid `:5353`** for the DNS port: it collides with mDNS/avahi on most
> desktops. That's why the default is `:8053`.

## Node identity vs. name keys

Two kinds of keys, kept separate on purpose:

- The **node's libp2p identity** (`~/.freedom/private.key`) identifies this peer
  on the network. A `private.key` in the working directory is still used if one
  is already there, so existing nodes keep their peer id.
- Your **name keys** live under `~/.freedom/keys/` and own your `.fn` names.

Because they're separate, your names are **portable**: you can publish them from
any node, and replacing a node doesn't change who owns your names.

## Kernel buffers (optional)

To avoid QUIC receive-buffer warnings from libp2p, raise the limits:

```sh
sudo sysctl -w net.core.rmem_max=7500000
sudo sysctl -w net.core.wmem_max=7500000
```

Persist them in `/etc/sysctl.conf`:

```ini
net.core.rmem_max=7500000
net.core.wmem_max=7500000
```

## Next

- [**Run a bootstrap node**](/examples/bootstrap-node) others can connect to.
- Back to [**running Freedom Names**](/guide/running-a-node).
