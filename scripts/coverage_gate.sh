#!/usr/bin/env bash
#
# S1-T31 coverage gate (AC-38). Fails CI when coverage drops below the thresholds.
#
#   - Overall  >= 70% over ./internal/... (the business-logic scope; generated
#     pkg/proto and the thin cmd/* entrypoints are NOT unit-tested logic and are
#     excluded — they are exercised by integration/T34).
#   - Security-critical packages >= 85% (the enforced set below).
#   - The DEFERRED list is now EMPTY: S1-T33 added unit tests that lifted
#     internal/agent/enroll and internal/agent/state to >=85% on the Linux gate, so
#     both were promoted into the enforced set. The mechanism is kept for future use.
#
# Run from the repo root:  ./scripts/coverage_gate.sh
set -euo pipefail

OVERALL_MIN=70
SECURITY_MIN=85

# Enforced security-critical packages (AC-38): signature verify, config precedence,
# redaction, hash-chain (+ the trust root and the update decision layer).
ENFORCED=(
  internal/updater/verify        # Ed25519 signature verification
  internal/agent/config          # config precedence
  internal/agent/logging         # secret redaction
  internal/agent/audit           # hash-chained audit
  internal/updater/trust         # update trust root
  internal/updater/manifestcheck # anti-rollback decision
  internal/agent/enroll          # enrollment use-case (S1-T33 lifted to >=85% Linux unit)
  internal/agent/state           # protected state store (S1-T33 lifted to >=85% Linux unit)
  internal/agent/license         # license signature verification (S1-T17)
)

# Deferred security packages: NONE. S1-T33 promoted internal/agent/enroll and
# internal/agent/state into ENFORCED above (both now >=85% on the Linux gate via
# unit tests). The array + loop are kept so a future package can be parked here with
# a documented rationale.
DEFERRED=()

pkg_cov() { # prints the integer-ish coverage % for a package, or "0.0" if no tests
  go test -cover "./$1" 2>/dev/null | grep -oE 'coverage: [0-9.]+%' | grep -oE '[0-9.]+' | head -1 || echo "0.0"
}

# ge A B -> success if A >= B (float-safe).
ge() { awk "BEGIN{exit !($1 >= $2)}"; }

echo "== coverage gate (overall >= ${OVERALL_MIN}%, security >= ${SECURITY_MIN}%) =="

# --- overall over the business-logic scope ---
go test -covermode=atomic -coverprofile=coverage.out ./internal/... >/dev/null
overall="$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$NF); print $NF}')"
status="OK"
fail=0
if ! ge "$overall" "$OVERALL_MIN"; then status="FAIL"; fail=1; fi
printf "  overall ./internal/... : %6s%%  [%s, min %s%%]\n" "$overall" "$status" "$OVERALL_MIN"

# --- enforced security packages ---
echo "  -- security-critical (enforced) --"
for p in "${ENFORCED[@]}"; do
  c="$(pkg_cov "$p")"
  s="OK"; if ! ge "$c" "$SECURITY_MIN"; then s="FAIL"; fail=1; fi
  printf "    %-32s %6s%%  [%s]\n" "$p" "$c" "$s"
done

# --- deferred security packages (informational; empty after S1-T33) ---
if [ "${#DEFERRED[@]}" -gt 0 ]; then
  echo "  -- security-critical (deferred; see scripts/coverage_gate.sh) --"
  for p in "${DEFERRED[@]}"; do
    c="$(pkg_cov "$p")"
    printf "    %-32s %6s%%  [DEFERRED < %s%%]\n" "$p" "$c" "$SECURITY_MIN"
  done
fi

if [ "$fail" -ne 0 ]; then
  echo "== coverage gate FAILED =="
  exit 1
fi
echo "== coverage gate PASSED =="
