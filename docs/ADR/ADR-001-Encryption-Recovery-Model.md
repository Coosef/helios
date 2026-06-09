# ADR-001 — Encryption & Recovery Model

- **Status:** Accepted (business-ratified)
- **Date:** 2026-06-08
- **Deciders:** Product Owner (business), Chief Architect, Security Lead
- **Related:** OQ-08, Technical Design §0.3, Risks SEC-1 / ARCH-5 / BKP-8 / RST-1 / GAP-1
- **Implementation sprint of the crypto itself:** Sprint 4 (this ADR only freezes the *model* and the Sprint-1 *enrollment/format reservations*)

## Context

Beyz Backup markets itself on **client-side encryption** so that "the server never accesses plaintext files" (SECURITY.md, CLAUDE.md). Taken literally, that implies a **zero-knowledge** design where the customer alone holds the key. But the target market — SMBs, hotels, MSPs, multi-location companies — is dominated by non-technical operators who **will lose passphrases**. In a pure zero-knowledge product, a lost key means **permanently unrestorable backups**: catastrophic for a *backup* product whose top guiding rule is "restore reliability has higher priority than backup speed" (CLAUDE.md).

This is the single highest-leverage irreversible decision in Sprint 1. Although the encryption engine ships in Sprint 4, the **enrollment exchange and on-disk/config format are frozen in Sprint 1**. If enrollment does not branch on the recovery model now, adding it later forces an enrollment-protocol break across a deployed, auto-updating fleet — and recovery can **never** be retrofitted onto data that was already encrypted under an unrecoverable key.

The business has ratified the direction: **Escrowed Recovery is the default; Zero-Knowledge is an opt-in mode; both must be representable from day one.**

## Decision

1. **`recovery_policy` is a first-class, immutable, per-device enrollment field**, captured at enrollment and bound into the device record. Enum:
   - `escrowed` — **DEFAULT**
   - `zero_knowledge` — opt-in

2. **Key hierarchy (implemented Sprint 4, modelled now):**
   - A per-device **Data Encryption Key (DEK)** is generated **on the device** and never leaves it in plaintext.
   - The DEK is wrapped by a tenant **Key-Encryption-Key (KEK)**. Only the **wrapped DEK** (`wrapped_device_key`) is ever stored locally or transmitted.
   - The **server never receives a plaintext DEK or plaintext file data** under either policy. "Zero-knowledge" is defined precisely as: *the server cannot derive the KEK.*

3. **Escrowed Recovery (default):** the KEK is escrowed by the Beyz SaaS, held **encrypted at rest** under a per-tenant envelope key in a managed KMS/HSM. The server can re-wrap the DEK for an authenticated recovery, so a customer who loses local material can still restore. The server still never sees plaintext **file** data — only the wrapped DEK and an escrow-protected KEK.

4. **Zero-Knowledge Mode (opt-in):** the KEK is derived **on the device** from a **customer recovery secret** via **Argon2id**, captured at enrollment. The server stores **no** recoverable KEK material. Enrollment in this mode **requires** an explicit recovery-material capture-and-confirm step (`recovery_material_ack = true`); without it, enrollment in `zero_knowledge` mode is refused. If the customer loses the recovery secret, backups are unrecoverable **by design**, and this is surfaced to the operator at enrollment.

5. **Sprint-1 reservations (frozen now, no crypto yet):** the enrollment request/response and `config.yaml`/state store reserve forward-compatible fields — `recovery_policy`, `recovery_material_ack`, `recovery_kdf_params`, `wrapped_device_key`, `key_id`, `key_wrap_version` — present (nullable) from the first installer so Sprint 4 needs **no** wire or on-disk break.

6. The precise meaning of "zero-knowledge" and the escrow trust boundary are documented in SECURITY.md as part of this ADR's rollout.

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| **Provider-escrowed master key only** (recoverable, *not* zero-knowledge) | Simplest and fully recoverable, but abandons the zero-knowledge promise entirely; unacceptable for security-sensitive tenants (legal, finance, MSP clients with compliance needs) and contradicts the product's stated positioning. |
| **Pure zero-knowledge only** (customer-held key, no escrow) | Honors the marketing claim but guarantees permanent data loss for the non-technical majority of the target market — the worst possible outcome for a backup product. High churn and liability risk. |
| **Defer the whole key model to Sprint 4, reserve nothing now** | Cheapest in Sprint 1, but freezes enrollment/config without the recovery branch, forcing an enrollment-protocol break across the fleet later and making recovery un-retrofittable onto already-encrypted historical data. Explicitly rejected in the Go/No-Go review. |
| **Single mode chosen globally (one or the other for all tenants)** | Removes the per-tenant flexibility MSPs and mixed customer bases need; a hotel chain and a law firm have different risk appetites. The `recovery_policy` field costs almost nothing and preserves both. |

## Consequences

**Positive**
- Recoverable-by-default protects the mass market from self-inflicted data loss while preserving restore-reliability-first.
- Security-sensitive tenants get true zero-knowledge as an explicit, informed opt-in.
- Both modes are non-breaking later because enrollment branches on `recovery_policy` and the format reserves the key fields **now**.
- The server-never-sees-plaintext-file-data invariant holds under both modes.

**Negative / costs**
- Two key-management code paths to build and test in Sprint 4 (escrow re-wrap vs. Argon2id-derived KEK).
- The escrow path requires a managed per-tenant KMS/HSM envelope-key service (Sprint 4 infrastructure) and a strong, audited recovery-authorization flow (a recovery is a sensitive operation and must itself be access-controlled and logged via the audit schema).
- Zero-knowledge users must be clearly informed that recovery is impossible without their secret; this is a UX and support obligation, not just a technical one.

**Sprint-1 impact (what this ADR requires *now*, before crypto exists)**
- The OpenAPI enrollment schema (S1-T02) includes `recovery_policy`, `recovery_material_ack`, `recovery_kdf_params`, and the reserved `wrapped_device_key`/`key_id`/`key_wrap_version` fields.
- `config.yaml` (S1-T07) and the state store (S1-T09) carry the same reserved fields (nullable), never holding plaintext key material.
- No DEK/KEK/Argon2id code is written in Sprint 1 (that is Sprint 4). Sprint 1 only persists the chosen `recovery_policy` and the reserved fields.
