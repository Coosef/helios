# ADR-003 — Agent Identity Model

- **Status:** Accepted
- **Date:** 2026-06-08
- **Deciders:** Chief Architect, Security Lead, Product Owner
- **Related:** OQ-07 / OQ-15, Technical Design §0.2 / §0.8 / §2, Risks ARCH-6 / ARCH-7 / SEC-5 / LIC-2 / LIC-6 / SCALE-4 / GAP-2
- **Implementation:** identity & enrollment land in Sprint 1; multi-tenant server-side isolation enforcement lands in Sprint 2 (the *binding* is frozen now).

## Context

The agent's identity is the binding point for **device licensing, the agent certificate, re-enrollment, multi-tenant isolation, and clone detection** — and all of it is frozen in Sprint 1 by the enrollment exchange and the installer rollout. Two failure modes must be avoided:

- A **hardware-hash fingerprint** breaks license binding and identity continuity on VM clone, disk re-image, or any hardware change — common in the hotel/SMB/MSP fleets we target (re-imaging is routine).
- A **config-file-only identity** makes a copied `config.yaml`/state indistinguishable from a legitimate device, defeating both licensing and tenant isolation.

"Multi-tenant first" is a stated principle, but tenancy must be **encoded into the identity itself** or it cannot be enforced from the first agent.

## Decision

1. **Primary identity = a server-issued opaque `device_id`**, minted by the SaaS at enrollment and bound into the agent certificate. It is **not** a client-computed hardware hash.

2. **Continuity anchor = a persisted, agent-generated random `device_guid` (UUIDv4)** stored at `C:\ProgramData\BeyzBackup\state\device.guid` (Linux: `/var/lib/beyz-backup/state/`). It is written once and **survives app reinstall**, providing stable identity across re-image/reinstall and enabling **license-seat reuse** on device replacement.

3. **Cryptographic identity = a locally-generated keypair + CSR** submitted at enrollment; the SaaS returns an agent certificate whose **SPKI thumbprint is the fingerprint**. The private key never leaves the device and is stored DPAPI-wrapped in the ACL-locked state store. This certificate is stored now as a client credential and becomes the **mTLS** identity in Sprint 8 with no enrollment-format change.

4. **Hardware signals are advisory only.** `MachineGuid` + primary disk serial + first NIC MAC are reported at enrollment **solely as clone-detection heuristics**, never as the license key or primary identity. Privacy: no usernames or file paths in the fingerprint.

5. **Tenancy is bound into identity at enrollment.** An immutable `tenant_id` (and `parent_org_id` for MSP hierarchies) plus a `region` claim are embedded in the agent certificate and authorized server-side on every request. `tenant_id` is also the future storage-key prefix and the per-tenant dedup boundary. Server-side isolation model = **shared-schema with `tenant_id` as the leading column of every composite key/index + row-level security** (enforced in Sprint 2; the *binding* is frozen now).

6. **Documented re-enrollment / device-replacement flow:** a cloned or re-imaged device whose presented cert/`device_guid` does not match the server's device record is detectable server-side and forced to re-enroll; a dedicated re-enrollment token reuses the existing license seat and `device_guid` while issuing a fresh certificate. (Flow documented in SECURITY.md; enforcement server-side in Sprint 2.)

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| **Client-computed stable-hardware hash as primary fingerprint** | Breaks on VM clone, disk re-image, NIC/disk replacement — exactly the events that are routine in the target fleets; turns every hardware change into a license/identity incident. |
| **Config-file `device_id` only, regenerated on reinstall** | A copied config is indistinguishable from a real device (no cryptographic identity), defeating licensing and tenant isolation; regeneration on reinstall loses continuity and seat reuse. |
| **No tenant binding in the cert (tenant tracked only server-side)** | Cannot authorize requests against an immutable tenant from the first agent; retrofitting `tenant_id`/`region` into already-issued certs is a fleet-wide flag day. |
| **Per-tenant schema/database isolation** | Strongest isolation but operationally heavy at SMB scale and overkill for Sprint 1; shared-schema + RLS + `tenant_id`-leading keys is the cheaper scalable model, with a documented sharding escape hatch for large/noisy tenants. |

## Consequences

**Positive**
- Stable identity across reinstall/re-image and hardware change; license-seat reuse works.
- Cryptographic, tamper-evident identity (CSR/cert) that upgrades to mTLS without a wire break.
- Tenant isolation and the storage-key/dedup boundary are enforceable from the first agent.
- Clone detection without privacy-invasive or brittle hardware-hash licensing.

**Negative / costs**
- Requires the SaaS to mint opaque `device_id`s and issue certs at enrollment (Sprint 2 server work; Sprint 1 validates against the mock).
- The re-enrollment / seat-reuse flow must be documented and implemented (token type + server logic in Sprint 2).
- Advisory hardware signals are heuristics, not guarantees — clone detection is best-effort.

**Sprint-1 impact**
- Enrollment payload (S1-T02 spec, S1-T14 client) carries `device_guid`, CSR, advisory hardware signals, and receives `device_id` + cert + immutable `tenant_id`/`parent_org_id`/`region`.
- `internal/agent/identity` (**S1-T11**) generates/persists the `device_guid`, keypair, CSR, and computes the SPKI-thumbprint fingerprint.
- No server-side RLS/tenant enforcement code is written in Sprint 1 (Sprint 2); only the binding is produced and persisted.

## Addendum (S1-T11 — Identity Architecture Review, approved 2026-06-09)

The following are **frozen** ahead of `internal/agent/identity` and are reflected in `api/openapi.yaml`:

1. **Key algorithm = ECDSA P-256** for the agent identity keypair (mTLS-interoperable across TLS 1.2/1.3; the CSR/cert algorithm is baked in — changing it later is a fleet re-key).
2. **No XOR fingerprint.** The three identity values are **separate and independent**: `device_guid` (UUIDv4 anchor), `device_id` (server-issued opaque primary), `spki_sha256` (credential fingerprint).
3. **SPKI fingerprint = `sha256(SubjectPublicKeyInfo_DER)`**, represented as **`sha256:<hex>`**. Sent as the optional `spki_sha256` request field (server cross-checks it against the CSR public key).
4. **Advisory hardware signals are privacy-preserving.** Raw machine GUID, disk serial, and NIC MAC are **never transmitted**; only deterministic `sha256:<hex>` hashes are sent (`machine_guid_sha256`, `primary_disk_serial_sha256`, `first_nic_mac_sha256`). They remain advisory clone-detection signals only, never the primary key.
5. **Identity key ≠ data-encryption key.** The T11 identity/mTLS key is re-issuable (no escrow); the data-encryption key (`wrapped_device_key` + `recovery_policy`, ADR-001) is a separate key with escrow/zero-knowledge recovery.
6. **Local-first generation** (GUID/key/CSR need no server) is preserved, keeping offline-enrollment compatibility open. The `device_guid` is stable across reinstall (write-once sidecar, T10).

Key rotation on renewal stays **flexible** (Sprint 1 keeps the key stable so the SPKI fingerprint is stable; rotation lands when the server tracks SPKI history).

## Note (S1-T14 — Enrollment crash recovery, 2026-06-09)

Enrollment crash recovery **does not change the identity model.** `device_guid` remains the **continuity anchor**: it (and the private key) are persisted locally *before* the server call, so they survive any crash. If the agent crashes after the server issues a credential but before it persists locally, the single-use token is already consumed (replay → `409`, fatal by design) and recovery is the **re-enrollment flow, which MUST reuse the existing `device_guid` and license seat** (decision 6 above; Technical Design §0.2/597) while the server issues a fresh certificate. Recovery is therefore an orchestration/server concern (tracked as IMPLEMENTATION-PLAN forward items FI-1/FI-2), not an identity-model change.
