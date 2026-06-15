# DR-06 — Audit Event Schema

- **Status:** Accepted — schema frozen & emitter implemented in Sprint 1 (Agent Foundation)
- **Date:** 2026-06-15
- **Related:** [ADR-004 — Protocol Versioning](../ADR/ADR-004-Protocol-Versioning.md) (versioned envelope); risks SEC-7 / GAP-7 / ARCH-9
- **Implementation:** `internal/agent/audit`, `internal/agent/state` (audit-spool), `internal/agent/logging` (`security.log`)
- **Captures the Sprint-1 decisions for:** the frozen, hash-chained security-event schema.

## Decision

Security events are recorded with a **frozen schema** and a **per-record BLAKE3 hash chain**, so the
record set is **tamper-evident**. The schema is fixed now because changing it later would invalidate
the integrity of historical records. Records are written to the `security.log` stream **and** mirrored
to the bbolt `audit-spool`; remote, off-device anchoring is Sprint 8.

## As-built (Sprint 1)

- **Frozen record** (fields fixed in number and order):
  `{schema_version, seq, prev_hash, this_hash, ts_local, ts_server, clock_skew_ms, source,
  event_type, category, severity, outcome, actor, tenant_id, parent_org_id, device_id,
  agent_version, target, trace_id, detail, signature, server_anchor_proof}`.
- **Hash chain:** `this_hash = BLAKE3(JCS(record))` where the canonical record **excludes**
  `{this_hash, signature, ts_server, server_anchor_proof}`. JCS (RFC 8785) makes the bytes
  reproducible; excluding those four fields lets the server fill them later **without** invalidating
  the chain.
- **Device-bound genesis (anti-graft):** the first record's `prev_hash` is
  `BLAKE3(device_guid + genesis-salt)`. Verification keyed to the wrong `device_guid` fails — a chain
  cannot be spliced from one device onto another.
- **Controlled vocabulary, enforced twice:** `event_type` must be in the frozen set
  (`enroll.*`, `auth.failure`, `cert.*`, `spki_pin.mismatch`, `update.*`, `config.*`, `license.*`,
  `integrity.*`, `restore.*`, `service.*`, `decommission.started`) at **emit** time **and** at
  **verify** time — an unknown type can only be injected by direct record manipulation, which
  verification then catches.
- **Verification** walks the chain and detects sequence gaps, reordering, deletion, tampering, and
  unknown event types (typed sentinel errors). The emitter resumes from the persisted chain head and
  serialises appends under a mutex so concurrent agent loops keep one consistent chain.
- **Redaction:** string values in `detail` and the full streamed JSON are passed through the
  redaction sink, so a token/key/path cannot leak into the audit trail (defense-in-depth — producers
  must still never put secrets in `detail`).

## Honest scope & deferrals

- **Tamper-evident, not tamper-proof.** A `SYSTEM`/admin attacker can recompute the local chain.
  True integrity requires **off-device WORM anchoring** (server-side), which lands in **Sprint 8**.
  Until then the local chain detects accidental corruption and non-privileged tampering.
- **Reserved (null in Sprint 1):** `signature` (filled post-enrollment) and `server_anchor_proof`
  (server WORM receipt); `ts_server` is server-filled. All are excluded from the hash, so filling
  them later changes nothing.
- Remote shipping, dedup, retention/legal-hold, and PII-redaction policy are Sprint 8.

## References

- Code: `internal/agent/audit/{audit.go,emitter.go,verify.go}`, `internal/agent/state/audit_appender.go`
- Related records: [DR-02](DR-02-enrollment-and-identity.md), [DR-03](DR-03-update-trust-and-rotation.md), [DR-04](DR-04-tenancy-and-isolation.md)
