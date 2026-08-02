#!/usr/bin/env bash
# Phase 3: content put on node B must be fetchable from node A, which never had
# the blob locally. Node A asks the DHT who provides the hash, dials B, streams
# the blob, and verifies it. Proves cross-peer content transfer (issue #1).
#
# Usage: run via run-all.sh, or standalone:
#   BIN=./freedom-names scripts/verify-network/02-content-fetch.sh

set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/lib.sh"

: "${BIN:?set BIN to the freedom-names binary}"
WORKDIR=${WORKDIR:-$(mktemp -d)}
HOME_A="$WORKDIR/home-a"; HOME_B="$WORKDIR/home-b"
mkdir -p "$HOME_A" "$HOME_B"

API_A="http://127.0.0.1:18420"
API_B="http://127.0.0.1:18421"

trap cleanup_nodes EXIT

step "Content: start bootstrap node A"
( cd "$WORKDIR" && env HOME="$HOME_A" \
    FREEDOM_HTTP_ADDR=127.0.0.1:18420 FREEDOM_DNS_ADDR=127.0.0.1:18053 \
    "$BIN" bootstrap >"$WORKDIR/node-a.log" 2>&1 ) &
NODE_PIDS+=("$!"); info "node A (bootstrap) pid $!"
wait_http "$API_A/health" 30 || fail "node A did not come up"
MADDR=$(bootstrap_multiaddr "$API_A")
[ -n "$MADDR" ] || fail "could not read node A multiaddr"
pass "node A healthy: $MADDR"

step "Content: start client node B, peered to A"
start_node "$WORKDIR/node-b.log" \
  HOME="$HOME_B" FREEDOM_BOOTSTRAP="$MADDR" \
  FREEDOM_HTTP_ADDR=127.0.0.1:18421 FREEDOM_AUTHORING_ADDR=127.0.0.1:18422 \
  FREEDOM_DNS_ADDR=127.0.0.1:18054
wait_http "$API_B/health" 30 || fail "node B did not come up"
wait_peers "$API_B" 60 || fail "node B never peered with A"
pass "nodes peered"

step "Content: put a page on node B"
PAGE="$WORKDIR/page.md"
printf '# hello from node B\n\nnonce %s\n' "$RANDOM$RANDOM" >"$PAGE"
env HOME="$HOME_B" "$BIN" freedom keygen page >/dev/null 2>&1 || true
env HOME="$HOME_B" "$BIN" freedom put page "$PAGE" --api "$API_B" >"$WORKDIR/put.log" 2>&1 \
  || fail "freedom put failed; see $WORKDIR/put.log"
cat "$WORKDIR/put.log"
# The "Uploaded … -> <hash>" line carries the content hash. Split on "-> " with
# awk (grep's -oE would treat the leading "->" as an option).
HASH=$(awk -F' -> ' '/Uploaded/{print $2; exit}' "$WORKDIR/put.log" | awk '{print $1}')
[ -n "$HASH" ] || { info "put output:"; cat "$WORKDIR/put.log"; fail "could not parse content hash from put output"; }
pass "put content, hash: $HASH"

step "Content: fetch the hash from node A (never stored locally)"
# Allow the provider record a moment to propagate to A's routing view.
OK=""
for i in $(seq 1 20); do
  if curl -fsS "$API_A/content?hash=$HASH" -o "$WORKDIR/fetched.md" 2>/dev/null; then
    OK=1; break
  fi
  sleep 3
done
[ -n "$OK" ] || fail "node A could not fetch the content (provider record not found / not peered)"

if cmp -s "$PAGE" "$WORKDIR/fetched.md"; then
  pass "node A fetched the exact bytes across the network — content transfer works"
else
  fail "node A fetched content but bytes differ from the original"
fi

step "Content: (bonus) full name→content path on A"
NAME=$(env HOME="$HOME_B" "$BIN" freedom name page 2>/dev/null || true)
if [ -n "$NAME" ] && curl -fsS "$API_A/resolve-content?name=$NAME" -o "$WORKDIR/resolved.md" 2>/dev/null && cmp -s "$PAGE" "$WORKDIR/resolved.md"; then
  pass "resolve-content on A returned the page for $NAME"
else
  warn "resolve-content not confirmed (name may not have replicated yet); core fetch already passed"
fi

step "Content: DONE"
info "workdir kept at $WORKDIR"
