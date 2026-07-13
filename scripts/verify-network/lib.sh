# Shared helpers for the live multi-node verification harness (issue #1).
# Sourced by the numbered step scripts; not meant to run on its own.
#
# Conventions:
#   - BIN         path to the freedom-names binary (built by run-all.sh)
#   - WORKDIR     scratch dir for logs, keys, and content
#   - PASS/FAIL   printed with a clear marker; FAIL exits non-zero.

set -u

# --- output ---------------------------------------------------------------

if [ -t 1 ]; then
  C_GREEN=$'\033[32m'; C_RED=$'\033[31m'; C_YELLOW=$'\033[33m'; C_DIM=$'\033[2m'; C_OFF=$'\033[0m'
else
  C_GREEN=; C_RED=; C_YELLOW=; C_DIM=; C_OFF=
fi

step()  { printf '\n%s== %s ==%s\n' "$C_DIM" "$*" "$C_OFF"; }
info()  { printf '  %s\n' "$*"; }
pass()  { printf '  %sPASS%s %s\n' "$C_GREEN" "$C_OFF" "$*"; }
warn()  { printf '  %sWARN%s %s\n' "$C_YELLOW" "$C_OFF" "$*"; }
fail()  { printf '  %sFAIL%s %s\n' "$C_RED" "$C_OFF" "$*" >&2; exit 1; }

# --- json ------------------------------------------------------------------
# Prefer jq; fall back to a tiny grep-based extractor for flat string fields
# so the harness still runs on a box without jq installed.

have_jq() { command -v jq >/dev/null 2>&1; }

# json_field <json> <key> : first string value for a top-level-ish key.
json_field() {
  local json=$1 key=$2
  if have_jq; then
    printf '%s' "$json" | jq -r --arg k "$key" '..|objects|.[$k]? // empty' 2>/dev/null | head -n1
  else
    printf '%s' "$json" | grep -oE "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -n1 | sed -E "s/.*:[[:space:]]*\"([^\"]*)\"/\1/"
  fi
}

# --- http / node -----------------------------------------------------------

# wait_http <url> <seconds> : poll until the URL answers 2xx, or time out.
wait_http() {
  local url=$1 secs=${2:-30} i=0
  while [ "$i" -lt "$secs" ]; do
    if curl -fsS -o /dev/null "$url" 2>/dev/null; then return 0; fi
    sleep 1; i=$((i+1))
  done
  return 1
}

# wait_peers <api> <seconds> : wait until /peers reports at least one peer.
wait_peers() {
  local api=$1 secs=${2:-45} i=0 n
  while [ "$i" -lt "$secs" ]; do
    n=$(curl -fsS "$api/peers" 2>/dev/null | grep -oE '"peers"[[:space:]]*:[[:space:]]*\[[^]]*\]' | grep -oE '"1[^"]+"' | wc -l | tr -d ' ')
    if [ "${n:-0}" -ge 1 ]; then return 0; fi
    sleep 2; i=$((i+2))
  done
  return 1
}

# bootstrap_multiaddr <api> : a complete dialable multiaddr for the node,
# i.e. a non-loopback /ip4/…/tcp/… listen address with /p2p/<peerID> appended.
# The listen address from /info may lack the /p2p/ suffix, so we add the peer id
# from /info ourselves to guarantee FREEDOM_BOOTSTRAP gets a full multiaddr.
bootstrap_multiaddr() {
  local api=$1 info addr pid
  info=$(curl -fsS "$api/info" 2>/dev/null) || return 1

  if have_jq; then
    addr=$(printf '%s' "$info" | jq -r '.listenAddresses[]?' 2>/dev/null \
      | grep -E '/tcp/' | grep -vE '/ip4/127\.|/ip6/::1' | head -n1)
  else
    addr=$(printf '%s' "$info" | grep -oE '/ip4/[0-9.]+/tcp/[0-9]+(/p2p/[A-Za-z0-9]+)?' \
      | grep -vE '/ip4/127\.' | head -n1)
  fi
  [ -n "$addr" ] || return 1

  case "$addr" in
    */p2p/*) printf '%s\n' "$addr" ;;   # already complete
    *)
      pid=$(json_field "$info" peerID)
      [ -n "$pid" ] || return 1
      printf '%s/p2p/%s\n' "$addr" "$pid"
      ;;
  esac
}

# start_node <logfile> <env-assignments...> : launch a node in the background,
# record its PID in NODE_PIDS. The node inherits the current dir's binary.
NODE_PIDS=()
start_node() {
  local log=$1; shift
  ( env "$@" "$BIN" >"$log" 2>&1 ) &
  NODE_PIDS+=("$!")
  info "started node (pid $!), log: $log"
}

# cleanup_nodes : kill every node we started. Registered on EXIT by callers.
cleanup_nodes() {
  local pid
  for pid in "${NODE_PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  # give them a moment to release ports/sockets
  sleep 1
}
