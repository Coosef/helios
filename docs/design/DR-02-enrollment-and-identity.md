# DR-02 — Enrollment & Agent Identity

- **Status:** Accepted — implemented in Sprint 1 (Agent Foundation)
- **Date:** 2026-06-15
- **Related ADRs:** [ADR-003 — Agent Identity Model](../ADR/ADR-003-Agent-Identity-Model.md), [ADR-006 — Location/Site Scoping](../ADR/ADR-006-Location-Site-Scoping-and-RBAC-Boundary.md)
- **Implementation:** `internal/agent/identity`, `internal/agent/enroll`, `internal/agent/app` (one-shot token)
- **Captures the Sprint-1 decisions for:** the identity model, the enrollment flow, and one-shot enrollment-token handling.

## Decision

The agent has a **three-part, independent identity** bound at enrollment, and consumes a
**single-use enrollment token** that is never written to config and never logged. Identity
material is generated **locally first** (no server needed) so possession is provable and the
private key never leaves the device.

| Value | Source | Role |
|---|---|---|
| `device_guid` | agent-generated UUIDv4, write-once `state/device.guid` sidecar | continuity anchor; survives reinstall / DB rebuild → license-seat reuse |
| `device_id` | server-issued, opaque | primary identity, certificate-bound |
| `spki_sha256` | `sha256(SubjectPublicKeyInfo)` of the ECDSA P-256 key | credential fingerprint |

There is **no XOR/composite fingerprint** — the three values are deliberately independent
(ADR-003 addendum).

## Enrollment flow (use-case orchestration, `internal/agent/enroll`)

1. **Ensure local identity** — `device_guid`, ECDSA P-256 keypair, CSR, SPKI fingerprint, and
   *hashed* advisory hardware signals.
2. **Emit `enroll.attempt`** to the tamper-evident audit chain (fail-closed: no audit → no call).
3. **`POST /v1/enroll`** with the token in the **body only**, a stable per-attempt
   `Idempotency-Key`, the CSR, `spki_sha256`, advisory `tenant_id`/`region`/`location_id` hints,
   and hashed hardware signals.
4. **Persist** server-authoritative values atomically — `device_id`, `tenant_id`,
   `parent_org_id`, `region`, `location_id`, `certificate` (non-secret) and
   `agent_session_token` + `license_blob` (secret, wrapped) — then emit `enroll.succeeded`.

**Status mapping:** `201` → enrolled; `401`/`409` → `ErrTokenRejected` (terminal, needs a fresh
token, security-logged, no silent re-enroll); `426` → `ErrUpgradeRequired` (route to the updater,
ADR-004). An audit-emit failure is *joined* to the outcome, never allowed to hide it.

**Enrolled predicate (fail-closed):** a device is enrolled only when `device_id` **and**
`certificate` **and** `agent_session_token` are all present — `device_id` alone is not enough.

## One-shot enrollment token

- **Source precedence:** `BEYZ_ENROLLMENT_TOKEN` wins; only when empty is the installer-dropped
  `state/enroll-token` file consulted. The token is **structurally absent from `config.yaml`**
  (the schema rejects it) and is **redacted at every log sink**.
- **Lifecycle:** the file is deleted on a **definitive outcome** (success / `401` / `409`) and
  **preserved on a transient failure** so a retry reuses the still-valid token. An
  empty/whitespace file is a poison-pill (deleted immediately); an unreadable file is preserved
  and treated as no-token (fail-closed).
- **Anti-hijack:** on an already-enrolled device a lingering token file is purged on start, so a
  planted file can neither linger nor force re-enrollment.

## Reserved / deferred

- **Re-enrollment / device-replacement** (reuse the same `device_guid` + license seat, fresh
  cert) is documented now; server enforcement lands in **Sprint 2** (forward items FI-1/FI-2).
- Hardware signals are **advisory clone-detection only**, transmitted as `sha256:<hex>` — never
  raw, never the license key (no PII).

## References

- [ADR-003](../ADR/ADR-003-Agent-Identity-Model.md), [ADR-006](../ADR/ADR-006-Location-Site-Scoping-and-RBAC-Boundary.md)
- Code: `internal/agent/identity/*`, `internal/agent/enroll/enroll.go`, `internal/agent/app/{app.go,enrolltoken.go}`
- Related records: [DR-01](DR-01-key-management.md), [DR-04](DR-04-tenancy-and-isolation.md), [DR-05](DR-05-protocol-versioning.md)
