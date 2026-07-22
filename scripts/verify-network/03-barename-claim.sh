#!/usr/bin/env bash
# Bare names: a funded claim -> whois cycle on a real BCH network. This is the ONE
# step that cannot be fully automated: it needs coins you control. Defaults to
# chipnet (free faucet coins). This proves the pure-Go CashTokens transaction is
# accepted by real BCH consensus — the single biggest unknown for v1.0.
#
# Usage (chipnet, free):
#   BIN=./freedom-names scripts/verify-network/03-barename-claim.sh
# Usage (mainnet, real coins):
#   FREEDOM_BCH_NETWORK=mainnet BIN=./freedom-names scripts/verify-network/03-barename-claim.sh myname
#
# It walks you through: show funding address -> you fund it -> claim -> whois.

set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/lib.sh"

: "${BIN:?set BIN to the freedom-names binary}"
export FREEDOM_BCH_NETWORK=${FREEDOM_BCH_NETWORK:-chipnet}
LABEL=${1:-fnverify$RANDOM}

step "Bare name: network = $FREEDOM_BCH_NETWORK, label = $LABEL"
if [ "$FREEDOM_BCH_NETWORK" = "mainnet" ]; then
  warn "MAINNET selected — this spends REAL BCH (a couple of dust outputs + fee)."
  printf '  Continue? [y/N] '; read -r ans
  case "$ans" in y|Y) ;; *) echo "  aborted."; exit 0;; esac
fi

step "Bare name: your Ed25519 owner key"
"$BIN" freedom keygen "$LABEL" >/dev/null 2>&1 || true
info "owner name: $("$BIN" freedom name "$LABEL")"

step "Bare name: funding wallet"
"$BIN" freedom wallet | tee "$WORKDIR/wallet.log" 2>/dev/null || "$BIN" freedom wallet
ADDR=$("$BIN" freedom wallet 2>/dev/null | grep -oE '(bitcoincash|bchtest):[a-z0-9]+' | head -n1 || true)
[ -n "$ADDR" ] && info "funding address: $ADDR"

cat <<EOF

  ${C_YELLOW}ACTION REQUIRED${C_OFF}
  Fund the address above, then press Enter to continue.
EOF
if [ "$FREEDOM_BCH_NETWORK" != "mainnet" ]; then
  cat <<EOF
  Chipnet faucets to try:
    - https://tbch.googol.cash/  (select chipnet)
    - search "BCH chipnet faucet" for current options
  A few thousand satoshis is plenty. Wait for 1 confirmation.
EOF
fi
printf '  Press Enter once the funding tx has confirmed… '; read -r _

step "Bare name: confirm the wallet sees the funds"
"$BIN" freedom wallet
printf '  Does the balance look funded? [y/N] '; read -r ans
case "$ans" in y|Y) ;; *) fail "wallet not funded yet — re-run once the faucet tx confirms";; esac

step "Bare name: claim $LABEL (broadcasts the mint tx)"
if "$BIN" freedom claim "$LABEL" 2>&1 | tee "$WORKDIR/claim.log"; then
  pass "claim broadcast without error (see txid above)"
else
  fail "claim failed — see $WORKDIR/claim.log (insufficient funds? no eligible genesis UTXO?)"
fi

step "Bare name: wait for confirmation, then whois"
info "polling whois (a claim must reach FREEDOM_BCH_MINCONF confirmations, default 1)…"
OK=""
for i in $(seq 1 40); do   # ~20 min at 30s; chipnet blocks are frequent
  OUT=$("$BIN" freedom whois "$LABEL.fn" 2>&1 || true)
  if printf '%s' "$OUT" | grep -qiE 'owner|pubKeyID|\.fn'; then
    printf '%s\n' "$OUT"; OK=1; break
  fi
  sleep 30
done
if [ -n "$OK" ]; then
  pass "whois resolved the on-chain owner — pure-Go CashTokens claim ACCEPTED by real consensus"
else
  fail "whois never resolved. Claim may still be unconfirmed; re-run whois manually:\n    $BIN freedom whois $LABEL.fn"
fi

step "Bare name: DONE"
