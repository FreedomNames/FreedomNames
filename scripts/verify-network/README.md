# Live multi-node verification harness (issue #1)

Scripts that verify the three capabilities that only exercise with real peers
connected, and cannot be proven by unit tests or a single node:

1. **Layer 1** — DHT record replication across independent nodes.
2. **Phase 3** — peer-to-peer content fetch (a node fetches a blob it never
   stored, via DHT provider records).
3. **Layer 2** — a funded `claim` → `whois` cycle on a real BCH network,
   proving the pure-Go CashTokens transaction is accepted by real consensus.

Legs 1 and 2 are **fully automated** (two nodes on one machine, isolated ports
and `HOME` dirs, peered over loopback). Leg 3 is **guided**: it needs coins you
control, so it walks you through funding and then runs and checks the claim.

## Quick start

From the repo root:

```sh
# automated legs only (Layer 1 + content):
scripts/verify-network/run-all.sh

# also run the guided Layer 2 claim (chipnet, free faucet coins):
scripts/verify-network/run-all.sh --with-l2
```

The harness builds the binary itself. Each leg brings up fresh nodes and tears
them down, prints `PASS`/`FAIL`, and ends with a summary. Logs and artifacts are
kept under a `/tmp/fn-verify.XXXX` workdir printed at the end.

## Requirements

- Go toolchain (to build), `bash`, `curl`.
- `jq` optional (a grep fallback is used if it is missing).
- For leg 3: a funded BCH address. Chipnet (the default) is free — see the
  faucet links the script prints. Mainnet spends real (tiny) BCH.
- Ports used on loopback: HTTP `8420`/`8421`, DNS `8053`/`8054`, libp2p `4020`
  (bootstrap). Free them or edit the scripts if they clash.

## Running a single leg

Each script is standalone; set `BIN` to a built binary:

```sh
go build -o /tmp/freedom-names .
BIN=/tmp/freedom-names scripts/verify-network/01-layer1-replication.sh
BIN=/tmp/freedom-names scripts/verify-network/02-content-fetch.sh
BIN=/tmp/freedom-names scripts/verify-network/03-layer2-claim.sh [label]
```

Run the Layer 2 leg on mainnet with real coins:

```sh
FREEDOM_BCH_NETWORK=mainnet BIN=/tmp/freedom-names \
  scripts/verify-network/03-layer2-claim.sh myrealname
```

## Two real machines

The automated legs peer two nodes over loopback, which exercises the same DHT
and content code paths as separate hosts. To verify across a real LAN/WAN
instead (recommended before declaring v1.0 done), follow the manual walkthrough
in [`website/docs/guide/testing-a-network.md`](../../website/docs/guide/testing-a-network.md):
run `freedom-names bootstrap` on machine A, point machine B at its multiaddr via
`FREEDOM_BOOTSTRAP`, and repeat the publish/resolve and put/fetch steps. The only
extra concern there is firewalling: the bootstrap's libp2p port must be reachable
(TCP, and UDP for QUIC).

## What a pass means for v1.0

- **Leg 1 PASS**: names replicate — Layer 1 works across nodes.
- **Leg 2 PASS**: content transfers peer-to-peer — Phase 3 works across nodes.
- **Leg 3 PASS**: a real, funded claim was accepted by BCH consensus and
  resolves — the highest-risk unknown is closed.

All three passing on real machines is the bar to drop the "treat production as
beta" caveat and cut v1.0.
