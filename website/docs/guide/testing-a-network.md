# Testing on a real network

Everything in Freedom Names is unit-tested, and the single-node paths are
verified end to end. But three capabilities only fully exercise on a **real
multi-node network** with peers actually connected: DHT record replication,
Bitcoin Cash claim broadcast, and peer-to-peer content fetch. A lone node has no
peers to replicate to, so those steps report "no peers" locally even though the
code is correct.

This page walks through validating each on real machines (or two terminals on
one machine with different ports). You need at least two nodes.

## Set up two nodes

On the machine that will host the network, run a **bootstrap** node:

```sh
./freedom-names bootstrap
```

Note its LAN multiaddr from the startup log, e.g.
`/ip4/192.168.1.10/tcp/4020/p2p/12D3KooW...` (or fetch the peer id with
`curl -s localhost:8420/info`).

On the second machine (or a second terminal with different ports), point a
client at it:

```sh
FREEDOM_BOOTSTRAP="/ip4/192.168.1.10/tcp/4020/p2p/12D3KooW..." \
FREEDOM_HTTP_ADDR="127.0.0.1:8421" \
FREEDOM_DNS_ADDR="127.0.0.1:8054" \
./freedom-names
```

Wait ~10-30 seconds, then confirm the DHT routing tables have populated (not just
a raw connection):

```sh
curl -s localhost:8420/peers   # bootstrap
curl -s localhost:8421/peers   # client
```

The client's `peers` array should list the bootstrap. (A client behind NAT is
not itself added to the bootstrap's table; that asymmetry is normal Kademlia
behaviour.)

## 1. Name replication (Layer 1)

Publish a name on one node, resolve it on the other:

```sh
# on the client
./freedom-names freedom keygen lantest
./freedom-names freedom set lantest A 203.0.113.7
./freedom-names freedom publish lantest --api http://localhost:8421

# on the bootstrap (separate node, separate cache)
NAME=$(./freedom-names freedom name lantest)   # or copy it from the client
./freedom-names freedom lookup "$NAME" --api http://localhost:8420
dig @127.0.0.1 -p 8053 "$NAME" A
```

Success: `publish` returns `Published ...` (not `failed to find any peer in
table`), and the bootstrap resolves a record it never saw locally.

If publish reports "no peers", the routing table has not converged yet: wait
longer and check `/peers`.

## 2. Bare-name claim (Layer 2, chipnet)

This proves the pure-Go CashTokens transaction passes real consensus. It needs
free chipnet coins. See the dedicated walkthrough:
[Claim a bare name](/examples/claim-a-bare-name). In short:

```sh
export FREEDOM_BCH_ELECTRUM=ssl://chipnet.bch.ninja:50002
./freedom-names freedom wallet            # fund the shown address from a faucet
./freedom-names freedom keygen mysite
./freedom-names freedom claim mysite      # broadcasts the mint tx
./freedom-names freedom whois mysite.fn   # after 1 confirmation
```

Success: `claim` returns a txid (no broadcast error), the NFT appears on a
chipnet token explorer, and `whois` resolves your key after confirmation.

## 3. Content fetch across peers (Phase 3)

Put content on one node, fetch it from another that does not have it locally:

```sh
# on the client: publish a page
echo "# hello from the client" > page.md
./freedom-names freedom put lantest ./page.md --api http://localhost:8421

# on the bootstrap: fetch the content hash it has never seen
HASH=<the hash freedom put printed>
curl "http://localhost:8420/content?hash=$HASH"
```

Success: the bootstrap returns the exact bytes. Under the hood it asked the DHT
which peers hold that hash, dialled the client, streamed the blob, and verified
its hash.

You can also test the full page-load path once the name has replicated:

```sh
curl "http://localhost:8420/resolve-content?name=$NAME"
```

## Troubleshooting

- **`failed to find any peer in table`** on publish/claim: the DHT routing table
  has not converged. Wait longer; verify with `/peers`. On a single isolated
  node this never resolves (there is nobody to replicate to) - that is expected.
- **Content fetch times out**: the provider record may not have propagated yet
  (allow a minute after `freedom put`), or the two nodes are not actually peered
  (check `/peers`).
- **Firewall**: the bootstrap's port `4020` (TCP, and ideally UDP for QUIC) must
  be reachable from the client.
