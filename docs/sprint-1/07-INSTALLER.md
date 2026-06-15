# S1-T29 — Windows Installer (Inno Setup)

> Status: implemented (unsigned). Sources: [`installer/beyz-backup.iss`](../../installer/beyz-backup.iss),
> [`installer/scripts/apply-acls.ps1`](../../installer/scripts/apply-acls.ps1).
> Static contract tests: [`installer/installer_test.go`](../../installer/installer.go).

The installer lays down both binaries, builds the ACL-locked `ProgramData` tree,
hands the agent its single-use enrollment token via the one-shot file, and
registers + starts the **single** `BeyzBackupAgent` service. It is **unsigned**
(Authenticode is S1-T30) but signing-ready.

## Layout (matches the agent/updater compiled path defaults)

| Location | Contents |
|---|---|
| `C:\Program Files\BeyzBackup\` | `beyz-backup-agent.exe`, `beyz-backup-updater.exe`, `scripts\apply-acls.ps1` |
| `C:\ProgramData\BeyzBackup\` | `config.yaml`, `state\`, `logs\`, `update\` |

## ACL model (set at folder-create time, fail-closed — Technical Design §0.4)

Locale-independent SIDs only (`S-1-5-18` SYSTEM, `S-1-5-32-544` Administrators,
`S-1-5-32-545` Users); inheritance broken on every node.

| Path | ACL |
|---|---|
| `BeyzBackup\` (root) | SYSTEM + Administrators **Full**; no Users |
| `state\` | SYSTEM + Administrators **only** |
| `update\` | SYSTEM + Administrators **only** |
| `logs\` | SYSTEM/Admin Full; **Users Read+Execute** |
| `config.yaml` | SYSTEM/Admin Full; **Users Read** |
| `state\enroll-token` | **SYSTEM only** (Administrators excluded) |

If `apply-acls.ps1` cannot prove the hardening (incl. a check that `state\` no
longer grants Users/Everyone) it exits non-zero and the install **aborts before
any credential is written**.

## Enrollment token handling

- **Silent (preferred / AC-33-safe):** `setup.exe /VERYSILENT /TOKENFILE=<path>` —
  only the *path* appears in the Inno command-line log, never the secret.
- **Silent (compat):** `/TOKEN=<value>` is supported, but ⚠️ Inno logs the full
  command line, so `/TOKEN=` leaks the value into `/LOG`. Use `/TOKENFILE=` for
  any logged/audited install.
- **Interactive:** a masked wizard page.
- The token is written to `state\enroll-token` via `SaveStringToFile` (a file API,
  **never** an Exec/`[Run]` parameter), then locked to **SYSTEM only**. It is
  **never** written to `config.yaml` and **never** logged. The agent (already on
  `main`) reads it on first-run enrollment and deletes it on consume.

## Service flow

Registration reuses the agent's own control verbs (kardianos):
`beyz-backup-agent.exe install --config <ProgramData>\BeyzBackup\config.yaml`
→ `sc config … start= auto` → `sc failure …` (restart actions) → `… start`.
Service name `BeyzBackupAgent`, **LocalSystem**, automatic start. The **updater
binary is installed but never registered as a service** (§0.6 / AC-42).

## Upgrade

`AppId` GUID keys the upgrade. `PrepareToInstall` stops the service before the
binaries are replaced; `state\` and `config.yaml` are preserved (`config.yaml`
is `onlyifdoesntexist`; `state\` is never in `[Files]`). `InitializeSetup`
refuses to install **over a newer** installed version (anti-rollback, REV-4). The
`update\` staging dir is laid out for T25/T27 (`current` / `new` / `backup`).

## Uninstall

Stops + removes `BeyzBackupAgent`, then defensively removes any `BeyzBackupUpdater`
scheduled task. Default **shreds state by removal** (DPAPI machine-scope secrets
become unrecoverable without the host key); **`/KEEPSTATE`** preserves `state\`
(+ `config.yaml`) for device replacement.

## Frozen decisions / deferrals

- **Updater scheduled task — DEFERRED (follow-up FI-T29-1).** Sprint 1 does **not**
  create an updater scheduled task; the updater runs on-demand (operator/agent
  triggered). The uninstaller still removes a `BeyzBackupUpdater` task if one ever
  exists (defensive no-op). When periodic updates are wired, the task creation +
  its removal-on-uninstall get a dedicated change + test.
- **Best-effort `/deenroll` on uninstall — DEFERRED (FI-T29-2).** `/deenroll` is
  **absent from the OpenAPI**, so the uninstaller does not call it. Add once the
  control-plane exposes the endpoint.
- **Authenticode — S1-T30.** Ships unsigned; the `SignTool=` hook is present and
  commented so T30 enables it without restructuring. Update trust remains the
  Ed25519 manifest signature, not Authenticode.

## Build / CI / validation

Inno Setup is **Windows-only** (`ISCC.exe`), so on non-Windows hosts the `.iss`
contract is validated by the **Go static tests** (`go test ./installer/...`) that
assert the layout, ACL, token, service, updater-not-a-service, and uninstall
invariants. CI adds a **non-blocking** `installer-build` job (windows runner,
pinned Inno Setup) that compiles the unsigned `BeyzBackupSetup-<ver>.exe` artifact.
It becomes a required gate only after **S1-T35 Pester** validates the produced
installer end-to-end (T35 is not required yet).
