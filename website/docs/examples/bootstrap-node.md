# Run a bootstrap node

A **bootstrap** node is a server-mode libp2p peer that other nodes connect to in
order to discover the rest of the network. If you're running Freedom Names across
several machines, stand up at least one.

## Start it

```sh
./freedom-names bootstrap
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
    "/ip4/203.0.113.10/tcp/4020",
    "/ip4/203.0.113.10/udp/4020/quic-v1",
    "/ip4/203.0.113.10/udp/4021/quic-v1/webtransport",
    "/ip4/203.0.113.10/udp/4022/webrtc-direct"
  ],
  ...
}
```

A bootstrap node listens on fixed p2p ports: **`4020`** (TCP and QUIC), plus `4021`
(WebTransport) and `4022` (WebRTC-direct) for browser peers. A full bootstrap
multiaddr combines a listen address with the peer ID:

```
/ip4/203.0.113.10/tcp/4020/p2p/<peerID>
```

::: tip
Use a **publicly reachable** address for a bootstrap node; other machines must be
able to open a connection to it. Make sure the relevant TCP/UDP ports are open.
:::

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
FREEDOM_BOOTSTRAP="/ip4/203.0.113.10/tcp/4020/p2p/<peerID>" ./freedom-names
```

You can list several for redundancy:

```sh
FREEDOM_BOOTSTRAP="/ip4/203.0.113.10/tcp/4020/p2p/<id1>,/ip4/203.0.113.11/tcp/4020/p2p/<id2>" \
./freedom-names
```

## Verify they connected

On any node, check the peers it knows about:

```sh
curl http://localhost:8420/peers
```

You should see the bootstrap node's peer ID appear in the routing table and
connected hosts as the network forms.

## Next

- Full env-var list in [Configuration](/guide/configuration).
- Back to the [Architecture](/guide/architecture) to see where the DHT peer fits.
