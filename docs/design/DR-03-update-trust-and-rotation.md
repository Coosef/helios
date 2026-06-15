# DR-03 — Update Trust, Ed25519 Signing & Key Rotation

- **Status:** Accepted — implemented in Sprint 1 (Agent Foundation)
- **Date:** 2026-06-15
- **Related ADRs:** [ADR-002 — Update Signing & Trust Model](../ADR/ADR-002-Update-Signing-Trust-Model.md)
- **Implementation:** `internal/updater/{trust,verify,manifestcheck,swap,healthgate,app}`, `pkg/manifest`
- **Captures the Sprint-1 decisions for:** the updater trust model, Ed25519 manifest signing, and key custody/rotation.

## Decision

Updates are trusted by a **real, enforcing Ed25519 signature** over the **RFC 8785 (JCS)
canonical manifest**, verified against a **compile-time embedded public-key *set*** selected by
`key_id`. The binary is trusted by its **dual BLAKE3 + SHA-256 hash taken only from the
signature-verified manifest**. Every failure is **fail-closed** with a precise, machine-readable
reason. This is production code, not a stub — CLAUDE.md forbids placeholder security and forbids
removing signature validation.

## Trust model (as-built)

- **Embedded key SET, not a single key.** Each manifest names a `key_id`; the updater accepts a
  signature from any embedded, **non-revoked** key. Revocation is twofold: compile-time embedded
  revocation **and** the signed manifest's `key_revocation_list`. This enables **overlapping-key
  rotation without re-issuing the fleet**.
- **Key custody.** The repo embeds the **public** key only
  (`internal/updater/trust/keyset.json`, `build/keys/update_pub_test.pem`). The **private** key
  lives in an offline HSM/KMS (production) or the CI secret manager (test) — never in the repo,
  never on a runner. A committed-private-key test + the `gitleaks` gate enforce this.
- **Rejection order, all fail-closed:** nil keyset → canonical/structural invalidity → malformed
  signature → unknown/revoked `key_id` → invalid signature → hash mismatch → anti-rollback.

## Verification & application (as-built)

- **Signature** covers the whole manifest minus the `signature` field; JCS makes the signed bytes
  deterministic across versions and **preserves unknown fields** (forward-compatible). `size_bytes`
  is bounded to 2⁵³−1 so the canonical float form is exact.
- **Hashes** are read **only** from the trusted manifest (never a header/filename); **both** SHA-256
  and BLAKE3 are required per artifact and compared in constant time.
- **Anti-rollback:** reject `target_version ≤ current_version` (absolute, from the signed manifest)
  unless a signed `allow_downgrade` flag is present; reject any agent **below `min_supported_version`**
  (a circuit-breaker floor). The kill-switch `update_allowed` is enforced; `rollout_cohort_pct` is
  parsed but not yet enforced (Sprint 2+).
- **Transport ≠ trust.** The manifest is fetched over the **SPKI-pinned** control channel; the
  binary may come from any HTTPS host because integrity comes from the signed hashes, not transport.
- **Atomic swap + health gate + rollback.** `MoveFileEx` atomic replace of the **agent** binary;
  a **90-second** health gate requires the new agent to report `update_result: ok` with a matching
  `update_id`, else the **BLAKE3-integrity-checked** backup is restored. The FSM persists
  `updater_state.json` before every side-effecting step so a crash resumes or rolls back to a
  consistent `(binary, config)` pair.

## Scope & deferrals

- The updater is an **on-demand binary, not a persistent service**. Updater **self-update** and a
  persistent watchdog are deferred to **Sprint 8** (§0.6).
- `released_at` is carried for future freshness/replay defense (not consulted in Sprint 1).
- Authenticode signing of the binaries/installer is a **separate** provenance concern (signing-ready
  in [DR — installer / ARCHITECTURE]), independent of this Ed25519 update-trust root.

## References

- [ADR-002](../ADR/ADR-002-Update-Signing-Trust-Model.md), `docs/sprint-1/08-SIGNING.md`
- Code: `internal/updater/{trust,verify,manifestcheck,swap,healthgate,app}`, `pkg/manifest`, `api/manifest.schema.json`
- Related records: [DR-01](DR-01-key-management.md), [DR-05](DR-05-protocol-versioning.md), [DR-06](DR-06-audit-event-schema.md)
