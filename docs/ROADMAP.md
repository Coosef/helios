# Roadmap

## Sprint 1

Agent Foundation

Status: Complete (P0)

Delivered:

* Windows Service (single `BeyzBackupAgent`, LocalSystem, SCM lifecycle + recovery)
* Config Management (YAML + JSON-Schema validation, env/flag precedence, secrets structurally absent)
* Structured Logging (zerolog JSON, rotation, redaction) + tamper-evident BLAKE3 audit chain
* Protected State Store (bbolt + DPAPI value-wrapping + ACL-locked folders + write-once device GUID)
* Identity & Enrollment (ECDSA P-256 + CSR + SPKI fingerprint; one-shot enrollment token)
* Heartbeat & Task-Poll (server-directed jittered cadence; forward-compatible envelopes)
* Hardened Transport (TLS 1.2+/1.3, mandatory SPKI pinning, backoff+jitter)
* Real Updater (enforcing Ed25519 manifest verify + BLAKE3/SHA256 + anti-rollback → stage → atomic
  swap → 90s health-gate → integrity-checked rollback; on-demand binary)
* Inno Setup Installer (dual-binary, ACL-hardened, single service; signing-ready)
* CI Security Gates (vet/lint/race/contract/drift/gitleaks/govulncheck + coverage gate + theme-G
  negative suite)

> Note: the **real, enforcing** signed-update verification and the **audit foundation** shipped in
> Sprint 1 (not Sprint 8). Sprint 8 adds only the deferred pieces — the persistent updater watchdog,
> two-stage self-update bootstrap, and off-device WORM audit anchoring.

---

## Sprint 2

SaaS Core

Status: Planned

Goals:

* Tenant Management
* User Management
* RBAC
* Agent Dashboard

---

## Sprint 3

Backup Engine V1

Status: Planned

Goals:

* File Discovery
* Full Backup
* Upload
* Manifest

---

## Sprint 4

Compression & Encryption

Status: Planned

Goals:

* ZSTD
* AES-256-GCM
* Key Management

---

## Sprint 5

Incremental Backup

Status: Planned

Goals:

* Chunk Engine
* Reference Counting
* Retention Policies

---

## Sprint 6

Restore Engine

Status: Planned

Goals:

* Single File Restore
* Folder Restore
* Device Restore

---

## Sprint 7

Storage Expansion

Status: Planned

Goals:

* SMB
* SFTP
* MinIO
* S3

---

## Sprint 8

Security Hardening

Status: Planned

Goals:

* Signed Updates
* mTLS
* Audit Logging

---

## Sprint 9

Reporting

Status: Planned

Goals:

* Email Alerts
* Reports
* Storage Warnings

---

## Sprint 10

Enterprise Features

Status: Planned

Goals:

* SQL Backup
* Docker Backup
* VSS
* Ransomware Detection
