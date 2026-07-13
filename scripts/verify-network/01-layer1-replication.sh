#!/usr/bin/env bash
# Layer 1: a name published on node A must resolve on node B via the DHT.
# Two nodes on this one machine, different ports and HOME dirs, peered over
# loopback. Proves cross-node record replication (issue #1, Layer 1 leg).
#
# Usage: run via run-all.sh, or standalone after building the binary:
#   BIN=./freedom-names scripts/verify-network/01-layer1-replication.sh

set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/lib.sh"

: "${BIN:?set BIN to the freedom-names binary}"
WORKDIR=${WORKDIR:-$(mktemp -d)}
HOME_A="$WORKDIR/home-a"; HOME_B="$WORKDIR/home-b"
mkdir -p "$HOME_A" "$HOME_B"

API_A="http://127.0.0.1:8420"
API_B="http://127.0.0.1:8421"

trap cleanup_nodes EXIT

step "Layer 1: start bootstrap node A"
# Node A is the bootstrap so B has a known peer to dial.
( cd "$WORKDIR" && env HOME="$HOME_A" \
    FREEDOM_HTTP_ADDR=127.0.0.1:8420 FREEDOM_DNS_ADDR=127.0.0.1:8053 \
    "$BIN" bootstrap >"$WORKDIR/node-a.log" 2>&1 ) &
NODE_PIDS+=("$!"); info "node A (bootstrap) pid $!"
wait_http "$API_A/health" 30 || fail "node A did not come up (see $WORKDIR/node-a.log)"
pass "node A healthy"

MADDR=$(bootstrap_multiaddr "$API_A")
[ -n "$MADDR" ] || fail "could not read node A's LAN multiaddr from /info"
info "bootstrap multiaddr: $MADDR"

step "Layer 1: start client node B, peered to A"
start_node "$WORKDIR/node-b.log" \
  HOME="$HOME_B" \
  FREEDOM_BOOTSTRAP="$MADDR" \
  FREEDOM_HTTP_ADDR=127.0.0.1:8421 \
  FREEDOM_DNS_ADDR=127.0.0.1:8054
wait_http "$API_B/health" 30 || fail "node B did not come up (see $WORKDIR/node-b.log)"
pass "node B healthy"

step "Layer 1: wait for the DHT routing tables to converge"
if wait_peers "$API_B" 60; then
  pass "node B sees at least one peer"
else
  fail "node B never populated its routing table (check firewall / logs)"
fi

step "Layer 1: publish on B, resolve on A"
LABEL="lantest$RANDOM"
env HOME="$HOME_B" "$BIN" freedom keygen "$LABEL" >/dev/null
env HOME="$HOME_B" "$BIN" freedom set "$LABEL" A 203.0.113.7 >/dev/null
NAME=$(env HOME="$HOME_B" "$BIN" freedom name "$LABEL")
info "name: $NAME"

if env HOME="$HOME_B" "$BIN" freedom publish "$LABEL" --api "$API_B" 2>&1 | tee "$WORKDIR/publish.log" | grep -q "^Published"; then
  pass "published from node B"
else
  fail "publish failed (routing table not converged?) see $WORKDIR/publish.log"
fi

# Node A has a separate cache and never saw this record locally.
info "resolving on node A (separate cache)…"
GOT=$(curl -fsS "$API_A/resolve?name=$NAME&type=A" 2>/dev/null || true)
if printf '%s' "$GOT" | grep -q "203.0.113.7"; then
  pass "node A resolved a record it never stored locally — replication works"
else
  fail "node A could not resolve the name. Response: $GOT"
fi

step "Layer 1: DONE"
info "workdir kept at $WORKDIR (logs, keys)"
