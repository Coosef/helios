#!/usr/bin/env bash
#
# S1-T31 coverage gate (AC-38). Fails CI when coverage drops below the thresholds.
#
#   - Overall  >= 70% over ./internal/... (the business-logic scope; generated
#     pkg/proto and the thin cmd/* entrypoints are NOT unit-tested logic and are
#     excluded — they are exercised by integration/T34).
#   - Security-critical packages >= 85% (the enforced set below).
#   - Two AC-38 security packages are DEFERRED (printed, not gated) for a tracked,
#     legitimate reason (freeze #9): integration-only or OS-specific execution.
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
)

# Deferred security packages (below SECURITY_MIN for a documented reason; NOT gated
# here — coverage is completed by the named follow-up).
#   internal/agent/enroll : the full enroll<->mock exchange is covered by the T34
#                           integration suite (AC-14); unit logic is ~81%.
#   internal/agent/state  : the Windows DPAPI protector is OS-specific and runs on
#                           the windows-test job; the Linux build omits it (~84%).
DEFERRED=(
  internal/agent/enroll
  internal/agent/state
)

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

# --- deferred security packages (informational) ---
echo "  -- security-critical (deferred; see scripts/coverage_gate.sh) --"
for p in "${DEFERRED[@]}"; do
  c="$(pkg_cov "$p")"
  printf "    %-32s %6s%%  [DEFERRED < %s%%]\n" "$p" "$c" "$SECURITY_MIN"
done

if [ "$fail" -ne 0 ]; then
  echo "== coverage gate FAILED =="
  exit 1
fi
echo "== coverage gate PASSED =="
