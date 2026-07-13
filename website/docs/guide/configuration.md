# Configuration

All configuration is via **environment variables**; nothing is hardcoded, so a
node is entirely driven by its environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `FREEDOM_HTTP_ADDR` | `127.0.0.1:8420` | HTTP API listen address (loopback by default) |
| `FREEDOM_DNS_ADDR` | `:8053` | DNS server listen address |
| `FREEDOM_UPSTREAM_DNS` | `1.1.1.1:53` | Upstream resolver for non-`.fn` queries |
| `FREEDOM_CONTENT_DIR` | `~/.freedom/content` | Content-addressed blobstore directory |
| `FREEDOM_BOOTSTRAP` | *(none)* | Comma-separated bootstrap peer multiaddrs |
| `FREEDOM_BCH_NETWORK` | `mainnet` | BCH network for Layer 2: `mainnet`, `chipnet`, `testnet4`, or `testnet3` |
| `FREEDOM_BCH_ELECTRUM` | *(built-in list per network)* | Comma-separated Electrum/Fulcrum servers, tried in order with failover (`ssl://host:port`). Overrides the built-in bootstrap list |
| `FREEDOM_BCH_MINCONF` | `1` | Confirmations before a name claim counts |

The HTTP API binds to **`127.0.0.1`** by default: it is an unauthenticated local
control surface (a browser or app spawns the node), so it must not be exposed on
all interfaces. Set `FREEDOM_HTTP_ADDR=:8420` to share it on a LAN deliberately.

A spawning host can also override these with **flags**, which take precedence
over the environment: `--http-addr HOST:PORT`, `--api-bind HOST`,
`--content-dir DIR`, `--dns-addr HOST:PORT`. See
[embedding a node](/guide/embedding).

The `FREEDOM_BCH_*` variables drive [Layer 2](/guide/layer2) (globally-unique
bare names on Bitcoin Cash), which is on by default on **mainnet**. The node
reaches the chain through a built-in per-network list of public Electrum Cash
(Fulcrum) servers, trying them in order with **failover**. Set
`FREEDOM_BCH_NETWORK` to a test network (`chipnet`, `testnet4`, `testnet3`) to
experiment with faucet coins, or `FREEDOM_BCH_ELECTRUM` to a comma-separated list
of your own servers. Layer 1 names always resolve regardless of these settings.

The DNS server defaults to the high port **`:8053`**, so a node runs **without
root**. If the DNS port can't be bound, the node logs a warning and keeps running;
the DHT and HTTP API are unaffected.

## Examples

**Local development** works out of the box on the default `:8053`, no `sudo` needed:

```sh
FREEDOM_HTTP_ADDR=127.0.0.1:8420 \
go run .
```

**Different upstream resolver**, forwarding non-`.fn` queries elsewhere:

```sh
FREEDOM_UPSTREAM_DNS=9.9.9.9:53 go run .
```

**Join a network via bootstrap peers**:

```sh
FREEDOM_BOOTSTRAP="/ip4/203.0.113.10/tcp/4001/p2p/<peerID>,/ip4/…/…" go run .
```

## System-wide resolution and the `:53` port

The default `:8053` is great for testing (`dig -p 8053 …`), but your OS and
browser only send DNS to the standard **`:53`**. To make `.fn` resolve
system-wide, run Freedom Names on `:53`. Since `:53` is privileged, either grant
the binary the capability once:

```sh
go build -o freedom-names .
sudo setcap cap_net_bind_service=+ep ./freedom-names
FREEDOM_DNS_ADDR=:53 ./freedom-names
```

…or keep `:8053` and forward `:53 → 127.0.0.1:8053` with a local resolver
(dnsmasq / systemd-resolved), or point a stub resolver at `127.0.0.1:8053`.

> **Avoid `:5353`** for the DNS port: it collides with mDNS/avahi on most
> desktops. That's why the default is `:8053`.

## Node identity vs. name keys

Two kinds of keys, kept separate on purpose:

- The **node's libp2p identity** (`private.key`) identifies this peer on the
  network.
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
- Back to [**running a node**](/guide/running-a-node).
