# S1-T30 — Windows version-info / Authenticode signing (signing-ready)

> Status: **signing-ready pipeline; Sprint-1 artifacts ship UNSIGNED.** A real
> Authenticode signature is a release-time step performed once a real
> code-signing certificate exists. Sources:
> [`Taskfile.yml`](../../Taskfile.yml) (`cross`/`dist`/`installer`/`gen:versioninfo`),
> [`installer/scripts/sign.ps1`](../../installer/scripts/sign.ps1),
> [`build/windows/sign-smoke.ps1`](../../build/windows/sign-smoke.ps1),
> [`build/windows/versioninfo.*.json`](../../build/windows/), the `sign-smoke` CI job.

## Authenticode vs the Ed25519 update trust root (do not confuse them)

| | Authenticode (T30) | Ed25519 manifest signature |
|---|---|---|
| Protects | install-time **provenance** of the `.exe`/installer | **update** decisions (which build to apply) |
| Trust anchor | a code-signing certificate (OS cert chain) | the compiled-in Ed25519 key**set** (`internal/updater/trust`) |
| Key in Sprint 1 | none (unsigned) | the **public** test key only; the private update key lives in the CI secret manager |

**Update decisions rely solely on the Ed25519 manifest signature, never on
Authenticode.** The Ed25519 update-signing private key is a separate concern and
is out of scope for T30.

## Sprint-1 default: UNSIGNED

There is no real certificate yet (purchasing one is out of scope), so `task dist`,
the `cross`/`installer-build` CI jobs, and the produced artifacts are **unsigned**.
`installer/scripts/sign.ps1` **skips** (exit 0) when no certificate is configured
— so unsigned builds keep working and PR CI needs no secrets.

## Version unification (single source: `VERSION`)

The repo-root [`VERSION`](../../VERSION) file is the **one** version source, shared by:
- Go `-ldflags` → `internal/buildinfo.Version` (+ `Commit`, `Date`, `Channel`),
- the Windows version-info resource (`.syso`),
- the Inno `AppVersion` (`ISCC /DAppVersion=<version>`),
- the artifact names (`BeyzBackupSetup-<version>.exe` — kept for T35).

A static test (`test/dist`) asserts the `.iss` fallback and the version-info JSONs
never drift from `VERSION`. `buildinfo.Channel` defaults to `dev` (overridable via
`-ldflags`/`task … CHANNEL=stable`) and appears in `--version`.

## Windows version-info

`task gen:versioninfo` runs the **pinned** `goversioninfo@v1.4.1` to generate
`cmd/{agent,updater}/resource_windows_amd64.syso` from `build/windows/
versioninfo.*.json`, driven by `VERSION`. The `.syso` files are **generated at
build time and gitignored — never committed**. The Go linker embeds the matching
`*_windows_amd64.syso` into the windows/amd64 binaries.

## Signing model (`installer/scripts/sign.ps1`)

Signs agent/updater/`setup.exe` with **SHA-256** + an **RFC3161 timestamp**, then
`signtool verify /pa`. Certificate from **environment only**:

- `BEYZ_SIGN_THUMBPRINT` — a cert already in the store (preferred; no password on disk), or
- `BEYZ_SIGN_PFX_BASE64` + `BEYZ_SIGN_PFX_PASSWORD` — decoded to a temp PFX deleted in `finally`.

**Fail-closed:** no cert + `-RequireSigned` ⇒ exit 2; cert present but sign/verify
fails ⇒ exit 1; no cert + not required ⇒ skip (unsigned). The password is **never
logged** and the `signtool` argument array (which carries `/p`) is never printed.
For the installer, enable the `.iss` `SignTool=` hook via `ISCC /Ssigntool=`.

## CI / release model

| Context | Signing | Secrets |
|---|---|---|
| `pull_request` (incl. forks) | none (unsigned) | **none** — CI references **no** signing secret |
| `sign-smoke` job (any safe context) | **self-signed** ephemeral cert | none |
| future release (tag / protected env) | **real** Authenticode | cert in a protected GitHub Environment |

The **`sign-smoke`** CI job proves the *entire* pipeline (version-info → build →
installer → sign → `signtool verify /pa`) using a throwaway self-signed cert that
it trusts (Trusted Root + Trusted Publisher) and then removes — **no real secret**.
It is non-blocking initially.

## Future: enabling real signing

1. Obtain a code-signing certificate (purchase / org CA). **Never commit it.**
2. Add `BEYZ_SIGN_PFX_BASE64` + `BEYZ_SIGN_PFX_PASSWORD` (or a runner cert +
   `BEYZ_SIGN_THUMBPRINT`) to a **protected GitHub Environment** (not repo PR CI).
3. Add a release job gated to tags/`release` on that environment that runs
   `sign.ps1 -RequireSigned` over the artifacts and `ISCC /Ssigntool=…` for the
   installer — **fail-closed**. Then publish.

## Out of scope (T30)

Real certificate purchase; production release **publishing**; the Ed25519
update-signing private key; EV/hardware-token signing; Phase 2/3 rebranding.
