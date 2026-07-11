# Run a bootstrap node

A **bootstrap** node is a server-mode libp2p peer that other nodes connect to in
order to discover the rest of the network. If you're running Freedom Names across
several machines, stand up at least one.

## Start it

```sh
go run . bootstrap
```

Or from the built binary:

```sh
./freedom-names bootstrap
```

## Find its address

Other nodes need this node's **multiaddr** (address + peer ID). Ask the node about
itself:

```sh
curl http://localhost:8080/info
```

```json
{
  "mode": "server",
  "peerID": "<peerID>",
  "listenAddresses": [
    "/ip4/203.0.113.10/tcp/4001",
    "/ip4/203.0.113.10/udp/4001/quic-v1"
  ],
  ...
}
```

A full bootstrap multiaddr combines a listen address with the peer ID:

```
/ip4/203.0.113.10/tcp/4001/p2p/<peerID>
```

::: tip
Use a **publicly reachable** address for a bootstrap node; other machines must be
able to open a connection to it. Make sure the relevant TCP/UDP ports are open.
:::

## Point other nodes at it

On each other node, set `FREEDOM_BOOTSTRAP` to a comma-separated list of bootstrap
multiaddrs:

```sh
FREEDOM_BOOTSTRAP="/ip4/203.0.113.10/tcp/4001/p2p/<peerID>" go run .
```

You can list several for redundancy:

```sh
FREEDOM_BOOTSTRAP="/ip4/203.0.113.10/tcp/4001/p2p/<id1>,/ip4/203.0.113.11/tcp/4001/p2p/<id2>" \
go run .
```

## Verify they connected

On any node, check the peers it knows about:

```sh
curl http://localhost:8080/peers
```

You should see the bootstrap node's peer ID appear in the routing table and
connected hosts as the network forms.

## Next

- Full env-var list in [Configuration](/guide/configuration).
- Back to the [Architecture](/guide/architecture) to see where the DHT peer fits.
