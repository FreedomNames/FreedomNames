#!/usr/bin/env bash
# Live multi-node verification harness for issue #1.
# Builds the binary, then runs the automated legs (Layer 1 replication + content
# fetch). The Layer 2 claim needs funded coins, so it is opt-in.
#
#   scripts/verify-network/run-all.sh            # automated legs only
#   scripts/verify-network/run-all.sh --with-l2  # also run the guided L2 claim
#
# Env:
#   FREEDOM_BCH_NETWORK=mainnet   run the L2 leg on mainnet (real coins)

set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
. "$HERE/lib.sh"

WITH_L2=""
for a in "$@"; do [ "$a" = "--with-l2" ] && WITH_L2=1; done

export WORKDIR=$(mktemp -d /tmp/fn-verify.XXXXXX)
export BIN="$WORKDIR/freedom-names"

step "Build the binary"
( cd "$ROOT" && go build -o "$BIN" . ) || fail "go build failed"
pass "built $BIN ($("$BIN" freedom name x >/dev/null 2>&1; echo ok))"

# Each leg brings its own fresh nodes up and tears them down, so ports are free
# between legs. Run them in sequence.
RESULTS=()

run_leg() {
  local name=$1 script=$2
  if WORKDIR="$WORKDIR/$name" bash -c "mkdir -p \"$WORKDIR/$name\"; WORKDIR=\"$WORKDIR/$name\" BIN=\"$BIN\" \"$script\""; then
    RESULTS+=("PASS  $name")
  else
    RESULTS+=("FAIL  $name")
  fi
}

run_leg "layer1-replication" "$HERE/01-layer1-replication.sh" || true
run_leg "content-fetch"      "$HERE/02-content-fetch.sh"      || true

if [ -n "$WITH_L2" ]; then
  step "Layer 2 claim (guided, needs funded coins)"
  # Forward FREEDOM_BCH_NETWORK (defaults to chipnet inside the leg) so
  # `FREEDOM_BCH_NETWORK=mainnet run-all.sh --with-l2` actually reaches the leg.
  mkdir -p "$WORKDIR/layer2"
  if WORKDIR="$WORKDIR/layer2" BIN="$BIN" \
     FREEDOM_BCH_NETWORK="${FREEDOM_BCH_NETWORK:-chipnet}" \
     "$HERE/03-layer2-claim.sh"; then
    RESULTS+=("PASS  layer2-claim")
  else
    RESULTS+=("FAIL  layer2-claim")
  fi
else
  info "skipping Layer 2 claim (pass --with-l2 to run it; it needs funded coins)"
  RESULTS+=("SKIP  layer2-claim (run with --with-l2)")
fi

step "Summary"
for r in "${RESULTS[@]}"; do
  case "$r" in
    PASS*) printf '  %s%s%s\n' "$C_GREEN" "$r" "$C_OFF" ;;
    FAIL*) printf '  %s%s%s\n' "$C_RED"   "$r" "$C_OFF" ;;
    *)     printf '  %s%s%s\n' "$C_YELLOW" "$r" "$C_OFF" ;;
  esac
done
info "logs + artifacts under $WORKDIR"

# Non-zero exit if any leg failed.
printf '%s\n' "${RESULTS[@]}" | grep -q '^FAIL' && exit 1 || exit 0
