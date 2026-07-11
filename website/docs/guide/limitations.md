# Known limitations

Freedom Names is a working system, but it is young. This page is an honest list
of what is not done, what is deliberately simple, and what to watch out for. None
of these are secrets: knowing the edges is part of using it well.

## Not yet verified on a live multi-node network

The single-node paths are verified end to end, and there are automated tests for
the wire protocols (including a real two-host libp2p content transfer). But three
capabilities only fully exercise with peers actually connected, and have not yet
been run on a large public network:

- **Name replication** across independent DHT nodes.
- **Bitcoin Cash claim broadcast** on chipnet/mainnet (a funded claim to whois
  cycle).
- **Peer-to-peer content fetch** via DHT provider records between two nodes.

The code for all three is complete and tested against mocks / in-process hosts.
See [testing on a real network](/guide/testing-a-network) to validate them
yourself. Treat production use as beta until you have.

## No public bootstrap network yet

`FREEDOM_BOOTSTRAP` is empty by default. Two nodes on the same LAN find each
other via mDNS, but nodes on different networks need at least one shared
bootstrap peer, and there is no official public one yet. Run your own bootstrap
node and share its multiaddr, or set `FREEDOM_BOOTSTRAP` explicitly. Until a
public bootstrap exists, off-LAN discovery is manual.

## Layer 2 (Bitcoin Cash) caveats

- **First-come, first-served, no stake.** The earliest confirmed claim for a bare
  name wins. There is no bidding or stake-weighting (unlike LBRY); a later,
  higher-value claim does not displace an earlier one. This is intentional for
  v1.
- **Single Electrum server by default.** Bare-name resolution reads the chain
  through one configured Electrum (Fulcrum) server. The default public server is
  a convenience for testing: it is a privacy leak (it sees every bare name you
  resolve) and a single point of failure. For real use, run or choose your own
  server via `FREEDOM_BCH_ELECTRUM`, and ideally cross-check multiple.
- **Light-client trust.** The resolver trusts the Electrum server's history
  responses; it does not verify SPV proofs. A malicious server could withhold or
  misreport claims. Mitigation (SPV proofs / multi-server agreement) is future
  work.
- **Sequential chain reads.** Resolving a bare name whose NFT has been
  transferred many times walks the custody chain hop by hop over a single
  connection. It is bounded (64 hops) and cached (5 min), but a heavily-traded
  name is slower to resolve than a fresh one.

## Content layer scope

- **Whole-blob, 32 MiB cap.** Each piece of content is stored and transferred as
  one blob, capped at 32 MiB. This comfortably covers pages and reasonable
  assets. Large media (chunking, deduplication, a DAG) is deliberately deferred.
- **No content garbage collection.** A node keeps and re-provides every blob it
  stores, including content it merely fetched for someone else. There is no
  retention policy or GC yet, so disk use grows with everything the node has
  seen. Prune `~/.freedom/content` manually if needed.
- **Availability follows uptime.** Content stays reachable only while a node that
  holds it is online and providing. There is no pinning service or replication
  guarantee; if every holder goes offline, the content is unreachable until one
  returns.

## Record and naming caveats

- **Records expire in 7 days.** A published record is valid for 7 days and must
  be re-published (re-signed) before then. The node re-provides it to the DHT
  while running, but it cannot re-sign (it does not hold your key), so an
  offline owner's name lapses after a week. Re-run `freedom publish` (or
  `freedom put`) to renew.
- **Long self-certifying names.** A self-certifying name carries a ~52-character
  key id (`label.<pubKeyID>.fn`). Bare names (Layer 2) avoid this but need a
  chain claim. A friendlier human-alias layer is future work.

## Operational notes

- **The HTTP API is unauthenticated.** It binds to `127.0.0.1` by default for
  this reason. Do not expose it on a untrusted network; anyone who can reach it
  can publish and fetch through your node.
- **DNS on `:53` needs privileges.** The default is the high port `:8053`. For
  system-wide `.fn` resolution, see [running a node](/guide/running-a-node#the-53-port).
