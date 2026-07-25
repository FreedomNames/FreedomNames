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
- **A funded Bitcoin Cash claim-to-whois cycle** end to end on a live network
  (mainnet or a test network), as opposed to against a mock chain.
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

## Bare-name (Bitcoin Cash) caveats

- **First-come, first-served, no stake.** The earliest confirmed claim for a bare
  name wins. There is no bidding or stake-weighting (unlike LBRY); a later,
  higher-value claim does not displace an earlier one. This is intentional for
  v1.
- **Public Electrum servers see your lookups.** Bare-name resolution reads the
  chain through Electrum (Fulcrum) servers. A node ships with a built-in
  per-network bootstrap list and fails over between them, so a single dead server
  is not a point of failure, but any public server still sees every bare name you
  resolve. For privacy, run your own Fulcrum and set `FREEDOM_BCH_ELECTRUM`.
- **Light-client trust.** The resolver trusts each Electrum server's history
  responses; it does not verify SPV proofs, and failover is for availability, not
  cross-checking. A malicious server could withhold or misreport claims.
  Mitigation (SPV proofs / multi-server agreement) is future work.
- **No reorg detection.** Correctness against chain reorganizations rests on
  `FREEDOM_BCH_MINCONF` confirmations plus the resolver cache (5 minutes for
  found owners, 30 seconds for not-found). A reorg deeper than the
  confirmation floor is simply not noticed until the cache entry expires and
  the chain is re-read.
- **Sequential chain reads.** Resolving a bare name whose NFT has been
  transferred many times walks the custody chain hop by hop over a single
  connection. It is bounded (64 hops) and cached (5 min), but a heavily-traded
  name is slower to resolve than a fresh one. Past 64 hops the walk stops
  *silently* and answers with the last owner it reached, an extremely traded
  name can resolve to a stale owner rather than an error.
- **Address history is scanned only so far.** Anyone can pay dust to a name's
  discovery marker, and every entry costs a round trip to an Electrum server, so
  a lookup reads at most 512 history entries per address. Which end it reads
  depends on what it is looking for: the earliest entries decide the winning
  claim, while a custody transfer is by nature at the recent end. A name
  deliberately spammed past the cap does not resolve to a stale or wrong owner —
  a scan that runs out of budget without an answer fails as inconclusive, is
  logged, and is *not* negative-cached, so the name resolves again once the
  history is back within reach. The name is still unresolvable while the flood
  is in scope, which makes this a denial-of-service avenue against a specific
  name rather than a hijacking one.

## Content layer scope

- **1 GiB content cap, flat chunking.** Content larger than 8 MiB is split into
  fixed-size chunks plus a manifest, up to 1 GiB total. Chunking is flat (one
  manifest level, no DAG), and chunk boundaries are fixed offsets, not
  content-defined: inserting bytes near the start of a file shifts every later
  boundary, so an edit re-uploads everything from the edit point onward.
  Cross-file deduplication only happens when chunk bytes align exactly.
- **Hosting is bounded, owned content is not.** Hosted (other people's) content
  is capped by `FREEDOM_CONTENT_HOST_BUDGET` with LRU eviction and a TTL, but
  content you published yourself is never evicted or size-capped locally;
  publishing a lot means hosting a lot yourself. That includes superseded
  versions: publishing an update is a new set with a new hash, and the old one
  stays owned (and pinned) until its blobs are removed by hand.
- **The index sidecar is trusting.** The hosting ledger is a plain
  `index.json` next to the blobs. If it is deleted or corrupted it is silently
  rebuilt from the blobs on disk, and every set, *including your own
  published content*, comes back as hosted (evictable). Re-publish to restore
  owned status.
- **Replication is best-effort, not guaranteed.** A publish pushes copies to 3
  peers and holders heal the count hourly, but there is no admission guarantee:
  on a small network, or if the closest peers are full or offline, fewer
  replicas may exist. If every holder of a set goes offline at once, the
  content is unreachable until one returns; the healing loop then restores
  the replica count from whichever holder survives.
- **Replicas trust their placement.** Any peer can push content within your
  budget; there is no per-peer quota yet (the pusher's peer ID is recorded for
  a future share cap), so a determined peer could fill another node's hosting
  budget with junk. Junk is displaced only under budget pressure, where
  TTL-expired and least-recently-used sets are evicted first.

## Record and naming caveats

- **Records expire in 7 days.** A published record is valid for 7 days and must
  be re-published (re-signed) before then. The node re-provides it to the DHT
  while running, but it cannot re-sign (it does not hold your key), so an
  offline owner's name lapses after a week. Re-run `freedom publish` (or
  `freedom put`) to renew.
- **Long self-certifying names.** A self-certifying name carries a ~52-character
  key id (`label.<pubKeyID>.fn`). Bare names avoid this but need a
  chain claim. A friendlier human-alias layer is future work.
- **Record sets are capped, and the caps are fixed.** A name carries at most 32
  resource records, a `TXT` value at most 255 bytes, a `CNAME` target 253, a
  label 190 (see [size limits](/guide/how-names-work#size-limits)). The caps are
  compile-time constants rather than a negotiated network parameter, so raising
  them later means every node has to agree at once.
- **The label is not part of resolution.** A name's DHT key comes from the
  pubkey id alone, so every label under one key resolves to that key's single
  record set: `blog.<pubKeyID>.fn` and `shop.<pubKeyID>.fn` return the same
  records. Use separate keys for genuinely separate names.

## Operational notes

- **The HTTP API is unauthenticated.** It binds to `127.0.0.1` by default for
  this reason. Do not expose it on a untrusted network; anyone who can reach it
  can publish and fetch through your node. It rejects domain-name `Host`
  headers, requests a browser marks as cross-site, and cross-origin requests,
  which stops a *web page* from driving it — but that is not authentication:
  anything that can open a socket to it still has full control. The cross-site
  check relies on `Sec-Fetch-Site`, which browsers older than roughly 2023 do
  not send; on those, an embedded `<img>` pointed at `/content` can still make
  your node fetch, keep and announce content of the page's choosing.
- **DNS forwarding is limited to local clients.** `.fn` is answered for anyone;
  everything else is only forwarded for loopback and private/link-local
  addresses, so a public node is not an open resolver. The classification is by
  address range alone — a spoofed source address in a private range is not
  detected, so a node on a hostile LAN should sit behind a firewall rather than
  rely on this. `FREEDOM_DNS_RECURSION=any` disables the restriction entirely.
- **A busy node sheds DNS load bluntly.** At most 64 `.fn` lookups walk the DHT
  at once; past that, queries get `SERVFAIL` and the client retries. That
  protects the node, but under sustained load some legitimate queries fail
  rather than queue.
- **DNS on `:53` needs privileges.** The default is the high port `:8053`. For
  system-wide `.fn` resolution, see [running Freedom Names](/guide/running-a-node#the-53-port).
