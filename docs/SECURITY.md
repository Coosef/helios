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
