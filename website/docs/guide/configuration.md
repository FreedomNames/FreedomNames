# Configuration

All configuration is via **environment variables**; nothing is hardcoded, so a
node is entirely driven by its environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `FREEDOM_HTTP_ADDR` | `:8080` | HTTP API listen address |
| `FREEDOM_DNS_ADDR` | `:53` | DNS server listen address |
| `FREEDOM_UPSTREAM_DNS` | `1.1.1.1:53` | Upstream resolver for non-`.fn` queries |
| `FREEDOM_BOOTSTRAP` | *(none)* | Comma-separated bootstrap peer multiaddrs |

## Examples

**Local development**, using unprivileged ports so you don't need `sudo`:

```sh
FREEDOM_DNS_ADDR=127.0.0.1:15353 \
FREEDOM_HTTP_ADDR=127.0.0.1:8080 \
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

## The `:53` port

`:53` is privileged. For local development use a high port
(`FREEDOM_DNS_ADDR=127.0.0.1:15353`), or grant the built binary the capability:

```sh
go build -o freedom-names .
sudo setcap cap_net_bind_service=+ep ./freedom-names
```

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
