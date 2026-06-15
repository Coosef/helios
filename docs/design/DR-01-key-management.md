# DR-01 — Key Management & Secret-at-Rest

- **Status:** Accepted — implemented in Sprint 1 (Agent Foundation)
- **Date:** 2026-06-15
- **Related ADRs:** [ADR-001 — Encryption & Recovery Model](../ADR/ADR-001-Encryption-Recovery-Model.md), [ADR-003 — Agent Identity Model](../ADR/ADR-003-Agent-Identity-Model.md)
- **Implementation:** `internal/agent/state`, `internal/agent/identity`
- **Captures the Sprint-1 decisions for:** secret storage, the data-encryption recovery model, and signing-key custody.

## Decision

Agent secrets live in a **machine-protected state store** (`internal/agent/state`): a single
bbolt file `state/agent-state.db` whose secret values are **wrapped before they are written**
and whose directory **ACL is the real access boundary**. The data-encryption key hierarchy is
**not generated in Sprint 1** — only its recovery model and wire/state surface are frozen
(ADR-001), so Sprint 4 can implement it without an enrollment-format break.

## Rationale

Two independent boundaries protect secrets at rest, defense-in-depth:

1. **The folder ACL / file mode is the primary boundary** against a live local attacker. It is
   set **at create time, fail-closed** (Windows: `SYSTEM` + `Administrators` only, `Users`
   removed, inheritance broken; POSIX: dir `0700`, files `0600`). If it cannot be applied,
   `Open()` fails — never best-effort.
2. **Value wrapping is defense-in-depth** against offline disk theft. On Windows secrets are
   wrapped with **DPAPI machine scope**; on non-Windows the default protector **fails closed**
   (`ErrUnsupportedProtection`) so plaintext secrets are *never* silently written (a real
   TPM/keyring protector is Sprint 8). Tests inject an in-memory `AES-256-GCM`
   `InsecureTestProtector`.

## As-built (Sprint 1)

- **Key classification is enforced at the store API.** Non-secret values (`device_id`,
  `tenant_id`, `parent_org_id`, `region`, `location_id`, `certificate`) go through `Put`/`Get`
  (plaintext); secrets (`agent_private_key`, `agent_session_token`, `license_blob`) go through
  `PutSecret`/`GetSecret` (wrapped). Calling the wrong method for a key returns `ErrInvalidState`.
- **Secret envelope** = `{protector, v, data}` JSON. A protector-name mismatch (e.g. a DB created
  under DPAPI opened with another protector) **fails closed** rather than returning garbage.
- **Atomic, ACID writes.** bbolt mutations are transactional; the `device.guid` sidecar uses
  write-temp → fsync → rename → dir-sync. A malformed/`0`/newer on-disk schema version fails closed.
- **Identity private key** = ECDSA P-256, stored PKCS#8-DER **wrapped** in the credentials bucket;
  it never leaves the device and is never logged.

## Reserved / deferred

- **Data-encryption key (DEK/KEK)** and `wrapped_device_key` / `key_id` / `key_wrap_version` are
  reserved (null) in Sprint 1; populated in **Sprint 4**. Recovery policy is frozen now —
  **escrowed by default**, **zero-knowledge** opt-in (ADR-001).
- **Update-signing private key** is *not* an agent key: it lives in an offline HSM/KMS (production)
  or the CI secret manager (test); the repo embeds the **public** key only — see
  [DR-03](DR-03-update-trust-and-rotation.md).
- **Linux/macOS production protector** (TPM/keyring) — Sprint 8.

## References

- [ADR-001](../ADR/ADR-001-Encryption-Recovery-Model.md), [ADR-003](../ADR/ADR-003-Agent-Identity-Model.md)
- Code: `internal/agent/state/{state.go,protector_windows.go,protector_other.go,guid.go}`
- Related records: [DR-02](DR-02-enrollment-and-identity.md), [DR-03](DR-03-update-trust-and-rotation.md), [DR-06](DR-06-audit-event-schema.md)
