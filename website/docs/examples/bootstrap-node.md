# Run a bootstrap node

A **bootstrap** node is a server-mode libp2p peer that other nodes connect to in
order to discover the rest of the network. If you're running Freedom Names across
several machines, stand up at least one.

## Start it

```sh
./freedom-names bootstrap
```

## Find its address

Other nodes need this node's **multiaddr** (address + peer ID). Ask the node about
itself:

```sh
curl http://localhost:8420/info
```

```json
{
  "version": "<version>",
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

A bootstrap node listens on fixed ports: **`4020`** (TCP and QUIC), plus `4021`
(WebTransport) and `4022` (WebRTC-direct) for browser peers. A full bootstrap
multiaddr combines a listen address with the peer ID:

```
/ip4/203.0.113.10/tcp/4020/p2p/<peerID>
```

::: tip
Use a **publicly reachable** address for a bootstrap node; other machines must be
able to open a connection to it. Make sure the relevant TCP/UDP ports are open.
:::

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
