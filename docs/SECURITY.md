# Security Requirements

## Encryption

Algorithm:
AES-256-GCM

Encryption occurs on the agent side.

Server must never access plaintext files.

---

## Authentication

Enrollment Token

Single-use only.

Agent Certificate

Generated after enrollment.

---

## Updates

All updates must be signed.

Agent must verify:

* Signature
* SHA256
* Version Manifest

before installation.

---

## Storage Security

Backup data must remain encrypted.

Storage compromise must not expose customer data.

---

## Restore Security

Integrity verification required before restore.

Corrupted backups must be quarantined.

---

## Logging

All security events must be logged.

Examples:

* Enrollment
* Update
* Restore
* Failed Integrity Check
* Authentication Failure

---

## Future Security Features

* Immutable Storage
* Ransomware Detection
* Audit Trail
* mTLS
* Hardware-backed keys

---

# Sprint-1 Security Model (As-Built)

The requirements above are product-level. This section documents the controls **implemented in Sprint
1**, cross-linked to the design records ([`design/`](design/)) and decision records ([`ADR/`](ADR/)).

## Secret-storage rule (reconciled)

CLAUDE.md says *"always use environment variables for sensitive configuration."* In this codebase that
rule means **no secrets in source or in `config.yaml`** — `config.yaml` carries only non-secret
operational settings, and its schema rejects an `enrollment_token` key. Runtime secrets are supplied
at run time and **never committed, never logged**:

- the **enrollment token** comes from `BEYZ_ENROLLMENT_TOKEN` or the installer's one-shot
  `state\enroll-token` file;
- the **agent private key / certificate / session token / license** live wrapped in the ACL-locked
  state store;
- the **update-signing private key** lives in an HSM/KMS (production) or the CI secret manager (test) —
  the repo embeds only the **public** keyset;
- the **Authenticode certificate** comes from a protected CI environment at release time.

The `Secret` wrapper renders `***REDACTED***` at every log sink (a token/key/path cannot leak into
`agent.log` or `security.log`).

## Token & credential lifecycle

- **Enrollment token** — single-use; consumed at `POST /v1/enroll`; the one-shot file is deleted on a
  definitive outcome (success / `401` / `409`) and purged on an already-enrolled device. `401`/`409`
  are terminal (a fresh token is required; no silent re-enroll). See
  [DR-02](design/DR-02-enrollment-and-identity.md).
- **Agent session token** — bearer credential returned at enrollment, wrapped at rest, rotated
  opportunistically; a `401` drives the agent to the degraded/re-enroll path.
- **Agent key/cert** — ECDSA P-256 key generated locally, never leaves the device; the certificate
  binds `tenant_id`/`parent_org_id`/`region`. mTLS lands in Sprint 8 with no enrollment-format change.

## Control-channel TLS & SPKI pinning

The control channel requires TLS 1.2+ (1.3 preferred) **and mandatory SPKI pinning** — a server
presenting a non-pinned certificate is **refused at the handshake** (system-CA-only trust is rejected).
The bootstrap pin set ships with the agent; rotation pins live in the ACL-locked state store, never
`config.yaml`. A TLS-intercepting proxy breaks pinning **by design** and must bypass the control-plane
FQDN. Fleet pin-rotation runbook is required before production (OQ-26). See
[ADR-005](ADR/ADR-005-Control-Channel-TLS-Pinning.md).

## Secret-at-rest: DPAPI + ACL

Two boundaries, defense-in-depth: the **NTFS ACL** (`SYSTEM` + `Administrators` only on `state\`,
`Users` removed, inheritance broken, set fail-closed at create time) is the **live-attacker boundary**;
**DPAPI machine-scope wrapping** of secret values is the **offline-disk-theft** defense. On non-Windows
the protector **fails closed** (no plaintext secrets) until the Sprint-8 TPM/keyring protector. A
tampered or wrong-protector blob fails closed. See [DR-01](design/DR-01-key-management.md) ·
[ADR-001](ADR/ADR-001-Encryption-Recovery-Model.md).

## Update trust (Ed25519)

Updates are gated by a **real, enforcing** Ed25519 signature over the RFC 8785 (JCS) canonical manifest,
verified against an embedded public-key **set** (`key_id` + revocation list), with **BLAKE3 + SHA-256**
binary hashes taken **only** from the signed manifest, **anti-rollback** + a `min_supported_version`
floor, atomic swap, a 90s health gate, and integrity-checked rollback. There is **no `return true`
stub** (enforced by code review + the `gitleaks`/coverage gates), and signature validation is never
removed (CLAUDE.md). See [DR-03](design/DR-03-update-trust-and-rotation.md) ·
[ADR-002](ADR/ADR-002-Update-Signing-Trust-Model.md).

## Audit hash-chain

Security events (enrollment, update, auth failure, config tamper, service lifecycle, …) are written to
`security.log` and a bbolt audit-spool with a **frozen schema** and a **per-record BLAKE3 hash chain**
anchored to a device-bound genesis (a chain cannot be grafted onto another device). The vocabulary is
enforced at emit and verify time. The chain is **tamper-evident** in Sprint 1; **tamper-proof**
off-device WORM anchoring is Sprint 8. Audit always writes regardless of operational log level. See
[DR-06](design/DR-06-audit-event-schema.md).

## Recovery policy & tenancy

- **Recovery model** (frozen, ADR-001): the data-encryption key is **escrowed by default** with a
  **zero-knowledge** opt-in; the key hierarchy itself is implemented in Sprint 4 (the wire/state surface
  is reserved now).
- **Tenant isolation:** immutable `tenant_id`/`parent_org_id`/`region` bound in the certificate;
  `device_id` (server-issued, not a hardware hash) is the primary identity; `location_id` is a mutable,
  non-cert-bound advisory scope. Server-side RLS enforcement is Sprint 2. See
  [DR-04](design/DR-04-tenancy-and-isolation.md).

## CI security gates (required checks)

Blocking, non-bypassable merge checks on `main`: `test` (+ coverage gate, ≥85% security pkgs / ≥70%
overall), `security` (`task test:negative` — the theme-G fail-closed suite), `gitleaks`
(secret + private-key scan), `lint` (gosec + staticcheck), `race`, `contract`, `drift`, `cross`,
`govulncheck`. **GitHub branch protection is a repository setting and must be enabled manually**; the
workflow provides the named jobs but cannot make itself required. Windows/Pester negatives run but are
non-blocking until stabilized. See `docs/sprint-1/{06-CI.md,09-SECURITY-GATES.md}`.
