# Installing the Helios Agent

> **Scope:** Sprint 1 (Agent Foundation). This installs the **agent + updater binaries**, the
> Windows service, and the enrollment bootstrap. There is no backup/restore engine yet (Sprints 3–6).
> Architecture detail: [ARCHITECTURE.md](ARCHITECTURE.md) · security model: [SECURITY.md](SECURITY.md)
> · design records: [design/](design/).

## What gets installed (Windows)

| Path | Contents |
|---|---|
| `C:\Program Files\BeyzBackup\` | `beyz-backup-agent.exe`, `beyz-backup-updater.exe` |
| `C:\ProgramData\BeyzBackup\config.yaml` | non-secret operator settings (no secrets — see below) |
| `C:\ProgramData\BeyzBackup\state\` | `agent-state.db` (bbolt), `device.guid`, transient `enroll-token` |
| `C:\ProgramData\BeyzBackup\logs\` | `agent.log`, `security.log` |
| `C:\ProgramData\BeyzBackup\update\` | update staging area |

- A **single** Windows service, **`BeyzBackupAgent`** (account `LocalSystem`, automatic start, with
  SCM failure-recovery actions), is registered and started.
- The **updater is installed as a binary but is NOT registered as a service** — it runs **on demand**
  (§0.6, AC-42). `sc query BeyzBackupUpdater` returns `1060` (does not exist).

## ACL model (set fail-closed at install time)

Inheritance is broken on every folder and locale-independent SIDs are used (`SYSTEM` `*S-1-5-18`,
`Administrators` `*S-1-5-32-544`, `Users` `*S-1-5-32-545`):

| Path | `SYSTEM` | `Administrators` | `Users` |
|---|---|---|---|
| `state\`, `update\` | Full | Full | — (removed) |
| `logs\` | Full | Full | Read + Execute |
| `config.yaml` | Full | Full | Read |
| `state\enroll-token` | Full | — | — |

If ACL hardening fails, **installation aborts before any credential is written** — there is no
partial-compromise state. The NTFS ACL is the live-attacker boundary; DPAPI value-wrapping is the
additional offline-theft defense (see [SECURITY.md](SECURITY.md) and [DR-01](design/DR-01-key-management.md)).

## Prerequisites

- Windows 10/11 or Windows Server 2016+ (x64).
- Administrator rights (the installer registers a `LocalSystem` service and sets ACLs).
- Outbound HTTPS to the control plane (enrollment is **online-only** in Sprint 1; the control channel
  is SPKI-pinned, so TLS-intercepting proxies must bypass the control-plane FQDN).
- A **single-use enrollment token** from the Helios panel.

## Interactive install

Run `BeyzBackupSetup-<version>.exe` and enter the enrollment token on the masked wizard page. The
agent enrolls on first start.

## Silent install

```bat
:: Preferred — pass the token by FILE (only the path is ever logged):
BeyzBackupSetup-<version>.exe /VERYSILENT /TOKENFILE="C:\path\to\token.txt" /LOG="install.log"

:: Also supported — pass the token inline (NOTE: avoid /LOG with /TOKEN; see below):
BeyzBackupSetup-<version>.exe /VERYSILENT /TOKEN=bzt_REPLACE_WITH_YOUR_TOKEN
```

### Enrollment-token handling (important)

- The token is written **once** to `state\enroll-token` via a raw file write (never as a command-line
  parameter to a logged step) and is **consumed and deleted** on the first definitive enrollment
  outcome. It is **never** placed in `config.yaml` and **never** appears in `agent.log` /
  `security.log`.
- Prefer **`/TOKENFILE=`** (only the path is logged). `/TOKEN=` is supported but its value can appear
  in the installer's own `/LOG` — do not combine `/TOKEN=` with `/LOG=` in production.
- At runtime the token may instead be supplied via the **`BEYZ_ENROLLMENT_TOKEN`** environment
  variable, which takes precedence over the file. See [DR-02](design/DR-02-enrollment-and-identity.md).

## Verify

```bat
sc qc BeyzBackupAgent      :: START_TYPE = AUTO_START, SERVICE_START_NAME = LocalSystem
sc query BeyzBackupAgent   :: STATE = RUNNING
sc query BeyzBackupUpdater :: 1060 — no second service (expected)
icacls C:\ProgramData\BeyzBackup\state   :: SYSTEM + Administrators only; no Users
```

The agent writes `agent.log` (operational, JSON) and `security.log` (hash-chained audit). Neither
ever contains the enrollment token, the session token, the private key, or the license blob.

## Upgrade & uninstall

- **Upgrade** preserves `state\` and `config.yaml`, stops the service before replacing binaries, and
  **refuses to install over a newer version** (anti-rollback).
- **Uninstall** stops and removes the `BeyzBackupAgent` service and, by default, **crypto-shreds**
  `state\` (the DPAPI-wrapped secrets become unrecoverable). Pass **`/KEEPSTATE`** to preserve the
  identity (`device_guid` + cert) for device replacement, which lets re-enrollment reuse the same
  license seat. `logs\` and `update\` are always removed.

## Signing status

Sprint-1 artifacts are **unsigned but signing-ready**: the Authenticode `signtool` step (S1-T30) is
wired and invocable with a certificate supplied via environment, but no production code-signing
certificate ships in Sprint 1. Authenticode provenance is **separate** from the Ed25519 update-trust
root ([DR-03](design/DR-03-update-trust-and-rotation.md), `docs/sprint-1/08-SIGNING.md`).

## Linux (preparation only)

The agent and updater cross-compile for `linux/amd64` and `linux/arm64`. A systemd unit and the
`/etc/beyz-backup` · `/var/lib/beyz-backup` · `/var/log/beyz-backup` path layout are **prepared**
(S1-T20) but Linux is not a Sprint-1 install target; on non-Windows the secret protector currently
fails closed (no plaintext secrets) pending the Sprint-8 TPM/keyring protector.
