# Run a bootstrap node

A **bootstrap** node is a server-mode libp2p peer that other nodes connect to in
order to discover the rest of the network. If you're running Freedom Names across
several machines, stand up at least one.

## Install and start

On a Debian or Ubuntu server, one command downloads a verified release, installs
the binary and the provided systemd unit, creates the dedicated `freedom` user,
and enables + starts the bootstrap service:

```sh
curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | \
  sudo bash -s -- bootstrap
```

This is deliberately a **bootstrap-only** installer. A normal node is a local,
foreground program and does not need systemd; use the [Quickstart](/guide/quickstart)
to download and run one.

### Manual installation

Prefer to inspect and install every file yourself? Download and verify the Linux
release as described in [Run Freedom Names](/guide/running-a-node#download-a-release),
then create the service account, install the binary, and open the unit file:

```sh
sudo useradd --system --create-home --home-dir /home/freedom \
  --shell /usr/sbin/nologin freedom
sudo install -m 755 freedom-names /usr/local/bin/freedom-names
sudoedit /etc/systemd/system/freedom-names-bootstrap.service
```

Copy this exact service definition into that editor. It is the same unit that
the installer deploys and that new Linux release archives include:

<<< @/../../deploy/freedom-names-bootstrap.service {ini}

`WorkingDirectory=/home/freedom` keeps the peer identity in the service user's
home rather than whichever directory an administrator happened to be in.

Save the file, then enable and start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now freedom-names-bootstrap
```

A bootstrap node differs from a normal node in three ways:

| | normal node | bootstrap node |
|---|---|---|
| p2p ports | ephemeral | fixed `4020`/`4021`/`4022` |
| HTTP API | `127.0.0.1:8420` | `127.0.0.1:8430` |
| DNS server | started (`:8053`) | **not started** |

The API is on a different port so a bootstrap node and a normal node can run on
one machine without either failing to bind, which matters if you also run
LibreWeb there, since it spawns its own node on `8420`. Both remain overridable
with `FREEDOM_HTTP_ADDR` / `--http-addr`.

A bootstrap node runs no DNS server: it is a rendezvous peer for others joining
the network, not a resolver for local clients. Use a normal node to resolve `.fn`.

## Find its address

Other nodes need this node's **multiaddr** (address + peer ID). Ask the node about
itself. Note the bootstrap API port:

```sh
curl http://localhost:8430/info
```

```json
{
  "version": "<version>",
  "role": "bootstrap",
  "mode": "Server",
  "peerID": "<peerID>",
  "listenAddresses": [
    "/ip4/127.0.0.1/tcp/4020",
    "/ip4/10.0.0.5/tcp/4020",
    "/ip4/172.17.0.1/tcp/4020",
    "/ip4/<PUBLIC_IP>/tcp/4020",
    "/ip4/<PUBLIC_IP>/udp/4020/quic-v1",
    "/ip4/<PUBLIC_IP>/udp/4021/quic-v1/webtransport/certhash/<hash>/certhash/<hash>",
    "/ip4/<PUBLIC_IP>/udp/4022/webrtc-direct/certhash/<hash>"
  ],
  ...
}
```

A bootstrap node listens on fixed p2p ports: **`4020`** (TCP and QUIC), plus `4021`
(WebTransport) and `4022` (WebRTC-direct) for browser peers. A full bootstrap
multiaddr combines a listen address with the peer ID:

```
/ip4/<PUBLIC_IP>/tcp/4020/p2p/<peerID>
```

::: tip
Use a **publicly reachable** address for a bootstrap node; other machines must be
able to open a connection to it. Make sure the relevant TCP/UDP ports are open.
:::

`listenAddresses` reports every address the host is bound to, which includes
private ones: loopback, LAN addresses, and Docker bridges like `172.17.0.1`.
Pick the publicly routable one. On a VPS with the public IP bound directly to the
interface it appears in the list; behind NAT it generally does not, so use the
address the outside world sees (`curl -4 ifconfig.me`) rather than anything
`/info` shows.

Share only the **TCP and QUIC** addresses as this node's stable contact points.
The WebTransport and WebRTC-direct addresses contain a `/certhash/…` component
derived from a self-signed certificate that libp2p regenerates on restart, so
they are correct only until the node restarts. Browser peers learn them
automatically once connected; never paste them into a config meant to outlive the
process.

## Keep the peer ID stable

A bootstrap node's peer ID is half its address. If the ID changes, every node
configured to reach it, including anything compiled into a release, is pointing
at a peer that no longer exists. The ID is derived from a private key on disk, so
keeping it stable is an operational task, not something the node handles for you:

- The installed service stores the key at
  **`/home/freedom/.freedom/private.key`**. **Back it up.** Losing it means a
  new peer ID and a silently unreachable bootstrap node.
- `WorkingDirectory=/home/freedom` is fixed in the shipped unit, so a stray
  `private.key` in an operator's shell directory cannot replace that identity.
- In a container, the key must live on a **mounted volume**. On an ephemeral
  filesystem every restart generates a fresh identity.
- Confirm it survived a restart: the peer ID from `/info` must be unchanged.

## Check it came up

```sh
sudo systemctl status freedom-names-bootstrap
sudo journalctl -u freedom-names-bootstrap -f
```

The log prints the peer ID and listen addresses at startup. Confirm the API
answers too:

```sh
curl http://localhost:8430/info
```

## Back up the identity key

It is generated on first start, so it does not exist until now:

```sh
sudo cp /home/freedom/.freedom/private.key ~/freedom-bootstrap-key.backup
```

Store that copy somewhere off the machine. It is the node's identity: without it
a rebuild gets a new peer ID and every node pointed at the old one fails to
connect.

Useful afterwards:

```sh
curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | \
  sudo bash -s -- bootstrap                       # verified upgrade + restart
sudo systemctl disable --now freedom-names-bootstrap
```

After a restart or reboot, re-check `/info`: the peer ID must be identical. If it
changed, the node is not reading the key you think it is, and the usual cause is a
different `WorkingDirectory` or a missing home directory.

## Getting listed as a default

`defaultBootstrapPeers` in `internal/config/config.go` is the list a fresh
install dials with no configuration. To be added, a node needs a **static IP**, a
**backed-up identity key**, and reachability verified from another machine. The
entries are raw `/ip4` multiaddrs on purpose: Freedom Names replaces DNS, so
bootstrapping via a DNS name would make joining the network depend on the system
it exists to replace. The trade-off is that a peer which moves needs a new
release, which is why the address has to be stable before it is committed.

## Firewall / ports

A bootstrap node enables UPnP port mapping (`NATPortMap`), so on many home
routers the ports open themselves. When UPnP is off or unavailable, forward these
four ports to the node **inbound**:

| Port | Protocol | Transport |
|---|---|---|
| `4020` | TCP | plain TCP |
| `4020` | UDP | QUIC |
| `4021` | UDP | QUIC WebTransport |
| `4022` | UDP | WebRTC-direct |

Port `4020` is needed on **both** TCP and UDP; they are separate transports that
share the number. So in total: TCP `4020`, and UDP `4020`, `4021`, `4022`.

Do **not** expose the HTTP API (`8430`): it binds to `127.0.0.1` and is an
unauthenticated control surface, so it must stay on loopback. A bootstrap node
runs no DNS server, so there is no DNS port to open either.

A normal node has nothing to forward: it uses ephemeral p2p ports and reaches the
network through this bootstrap node's relay and hole-punching, which is why the
bootstrap node is the one that needs a public address and open ports.

## Point other nodes at it

On each other node, set `FREEDOM_BOOTSTRAP` to a comma-separated list of bootstrap
multiaddrs:

```sh
FREEDOM_BOOTSTRAP="/ip4/<PUBLIC_IP>/tcp/4020/p2p/<peerID>" ./freedom-names
```

You can list several for redundancy:

```sh
FREEDOM_BOOTSTRAP="/ip4/<PUBLIC_IP>/tcp/4020/p2p/<id1>,/ip4/<PUBLIC_IP_2>/tcp/4020/p2p/<id2>" \
./freedom-names
```

## Verify they connected

On any node, check the peers it knows about:

```sh
curl http://localhost:8420/peers
```

You should see the bootstrap node's peer ID appear in the routing table and
connected hosts as the network forms.

For a real check, run this from a **different machine**. A bootstrap node that
works only from its own host is not reachable. Give the test node exactly one
multiaddr so a success can only mean that transport worked:

```sh
FREEDOM_BOOTSTRAP="/ip4/<PUBLIC_IP>/tcp/4020/p2p/<peerID>" \
FREEDOM_HTTP_ADDR=127.0.0.1:8499 FREEDOM_DNS_ADDR=:8099 ./freedom-names
```

The distinct ports let this run alongside an existing node. Repeat with the QUIC
address (`/udp/4020/quic-v1/p2p/<peerID>`) to check that transport separately.
TCP and UDP `4020` are forwarded independently, so one can work while the other
is blocked.

The log line to look for is:

```
Event: 'Peer identification completed' - <peerID>
```

That is the meaningful signal. An open port only proves something is listening;
identify completing proves the host actually holds the private key for that peer
ID, so the multiaddr is correct end to end. A plain `nc -vz <PUBLIC_IP> 4020` is a
useful first check but cannot confirm the peer ID.

`networkSize: 0` on a two-node network is expected, because the estimator needs a
fuller routing table. Judge success by the peer appearing in `/peers`, not by that
figure.

## Next

- Full env-var list in [Configuration](/guide/configuration).
- Back to the [Architecture](/guide/architecture) to see where the DHT peer fits.
