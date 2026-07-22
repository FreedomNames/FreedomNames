#!/usr/bin/env bash
# Live multi-node verification harness for issue #1.
# Builds the binary, then runs the automated legs (self-certifying replication +
# content fetch). The bare-name claim needs funded coins, so it is opt-in.
#
#   scripts/verify-network/run-all.sh                   # automated legs only
#   scripts/verify-network/run-all.sh --with-bare-names # also run the guided claim
#
# Env:
#   FREEDOM_BCH_NETWORK=mainnet   run the bare-name leg on mainnet (real coins)

set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
. "$HERE/lib.sh"

WITH_BARE_NAMES=""
for a in "$@"; do [ "$a" = "--with-bare-names" ] && WITH_BARE_NAMES=1; done

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

run_leg "selfcert-replication" "$HERE/01-selfcert-replication.sh" || true
run_leg "content-fetch"        "$HERE/02-content-fetch.sh"        || true

if [ -n "$WITH_BARE_NAMES" ]; then
  step "Bare-name claim (guided, needs funded coins)"
  # Forward FREEDOM_BCH_NETWORK (defaults to chipnet inside the leg) so
  # `FREEDOM_BCH_NETWORK=mainnet run-all.sh --with-bare-names` reaches the leg.
  mkdir -p "$WORKDIR/barename"
  if WORKDIR="$WORKDIR/barename" BIN="$BIN" \
     FREEDOM_BCH_NETWORK="${FREEDOM_BCH_NETWORK:-chipnet}" \
     "$HERE/03-barename-claim.sh"; then
    RESULTS+=("PASS  barename-claim")
  else
    RESULTS+=("FAIL  barename-claim")
  fi
else
  info "skipping bare-name claim (pass --with-bare-names to run it; it needs funded coins)"
  RESULTS+=("SKIP  barename-claim (run with --with-bare-names)")
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
