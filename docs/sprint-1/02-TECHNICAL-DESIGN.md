# Helios — Sprint 1 Technical Design

> Sprint 1 = **Agent Foundation** (Windows Service, config, logging, enrollment, heartbeat, task-poll placeholder, updater, Inno Setup installer). No backup/restore/crypto engine. Author: Lead Software Architect. Date: 2026-06-08. Status: for sign-off before coding.

**Reading order:** §0 is authoritative and overrides any conflicting detail in §1–§5. §1–§5 are the detailed working design (produced by parallel design tracks); §0 resolves the contradictions and gaps a completeness review found across them.

## §0. Architect's Binding Decisions & Reconciliations (authoritative)

This is the single source of truth. The detailed working design in §1–§5 was produced by independent design tracks that disagreed on several specifics (logging/config/hash libraries, endpoint count, Go version, state-store location) and deferred a few decisions that Sprint 1 actually freezes. The decisions below **resolve those conflicts and override §1–§5 wherever they differ.** Settle these before writing code.

### 0.1 Dependency & format reconciliations (one-line decisions that must not be left split)

| Concern | BINDING DECISION | Supersedes |
|---|---|---|
| Logging library | **`rs/zerolog`**, behind an internal `Logger` interface (DI) so it is swappable | the "`slog`/`zap`" mention in §5 |
| Config library | **`knadh/koanf` + `yaml.v3` + `santhosh-tekuri/jsonschema/v6`**; hot-reload (if built) via koanf file-provider + `fsnotify` — **not Viper** (global state, heavier surface, unwanted remote backends in a security-sensitive agent) | the `spf13/viper` choice in §3 |
| BLAKE3 library | **`github.com/zeebo/blake3`** (asm-accelerated, well-maintained) | `lukechampine.com/blake3` in §4 |
| Content-hash policy | BLAKE3 for the backup/content path (tag `blake3:`); **SHA256 for the update-binary path** (tag `sha256:`) per SECURITY.md; both algorithm-tagged so either can evolve without breaking verification | — |
| Control-plane endpoints | **6 endpoints** in the OpenAPI contract: `enroll`, `register`, `heartbeat`, `tasks`, `tasks/{id}/ack`, `tasks/{id}/status`. The **Sprint-1 happy path exercises 4** (enroll → register → heartbeat → tasks); `ack`/`status` are mocked and exercised only by the `noop` lease/ack round-trip test | the "four endpoints" phrasing used elsewhere |
| Go version | **Go 1.23.x floor**, pinned in `go.mod`; CI builds/tests on **1.23.x only** (OS matrix = windows + linux). No 1.22 in the matrix — a split floor invites the mixed-toolchain drift the review warns about | the "1.22/1.23 matrix" in §5 |
| Agent state store | **bbolt single file `state\agent-state.db`**, secret values DPAPI-wrapped *inside* it. The **private key + cert live in bbolt as wrapped values, NOT as loose `agent.key`/`agent.crt` files.** `state\device.guid` is the one exception: a separate write-once plaintext file so it survives a rebuilt DB | the "loose `agent.crt`/`agent.key`/`device.key.dpapi` files" layout in §5 |
| Updater state store | **standalone `state\updater_state.json`** (atomic write-temp-rename), **owned by the updater process — never a bucket inside the agent's bbolt** (a separate process cannot share bbolt's single-writer lock) | the bbolt `updater` bucket in §3 |

### 0.2 Sprint-1 auth credential & cert renewal — closing the 30-day time-bomb

§2 referenced `agent_session_token` with no lifecycle, and set `cert_not_after` ≈ 30 days with renewal "via heartbeat" but no renewal endpoint. **Binding decision:**

- Non-enroll calls authenticate with a **bearer `agent_session_token`** issued in the `POST /enroll` response. **TTL = 24h.** It is **refreshed opportunistically**: any heartbeat response MAY return a rotated `agent_session_token`, which the agent swaps in atomically. The server can revoke it (next call → `401` → renew/re-enroll). Stored DPAPI-wrapped in bbolt.
- The enrollment-issued **client certificate is stored now but NOT used for transport in Sprint 1.** It is the seed for **mTLS promotion in Sprint 8 with no enrollment-format change.**
- **Cert/credential renewal is a Sprint-1 deliverable, not a gap.** `POST /agents/{id}/register` is the **renewal endpoint**: when `now > cert_not_after − renewal_window` (default 7 days) the agent generates a fresh CSR and calls `register` to get a new cert + session token. A 30-day cert **with** a working renewal path is fine; **without** one it is a guaranteed fleet outage. Renewal is wired (and mock-tested) in Sprint 1.

### 0.3 Encryption-key & recoverability model — DECIDED NOW (not deferred to a design record)

The review's #1 irreversible item, and the one place "reserve a null field" is **not** sufficient: zero-knowledge purity vs. recoverability for non-technical SMB / hotel customers who *will* lose passphrases. The protocol shape is the irreversible part. **Binding decision:**

- The enrollment payload carries a **`recovery_policy` field from day one** — enum `escrowed` (default) | `zero_knowledge`.
- **Default `escrowed`:** the per-device data key (generated client-side in Sprint 4) is wrapped by a tenant KEK the SaaS escrows. The server still never sees **plaintext file data** — it holds only the *wrapped* key. This is **recoverable**, which is the right default for the SMB / hotel / MSP market (lost passphrase = lost backups = churn and liability).
- **Opt-in `zero_knowledge`:** the KEK derives from a **customer recovery secret captured at enrollment.** Because that changes the enrollment **UX** (a recovery-code capture + confirm step), the enrollment flow **reserves that branch now** via `recovery_material_ack` (bool) + `recovery_kdf_params`, even though the crypto lands in Sprint 4.
- **The decision Sprint 1 freezes is "enrollment branches on `recovery_policy`,"** not the crypto itself. The default and the zero-knowledge opt-in both become non-breaking later. This is the top item needing **product/business sign-off** (see Open Questions OQ-blocking). Architecture recommendation: **escrowed-default with opt-in zero-knowledge.**

### 0.4 Secret-at-rest threat model correction — DPAPI ≠ ACL (write this into SECURITY.md)

§3/§5 over-stated DPAPI as *the* secret boundary. **Binding clarification:**

- **DPAPI machine-scope protects against *offline disk theft only.*** Any process running as **SYSTEM or local Administrator can unwrap** a machine-scoped blob. Against a live local-admin/SYSTEM attacker, the **NTFS ACL (SYSTEM + Administrators only, `Users` removed) is the real boundary**, not DPAPI.
- Therefore ACLs are **mandatory and set at folder-create time** (not best-effort, not post-hoc); DPAPI is defense-in-depth for the stolen-disk case. **TPM/CNG hardware-backed keys** are the Sprint-8+ upgrade for live-attacker resistance.
- **Local hash-chained audit logs are tamper-*evident* only once server-side shipping exists** — a SYSTEM attacker can recompute the local chain. Until Sprint 8 shipping lands, the chain is a **schema decision, not a live control.** Document it honestly; do not claim a mitigation that is not yet there.

### 0.5 SPKI pin location & rotation

§2 put the pin in `config.yaml`; §3 put it in the state store. A pin in operator-editable config is both inconsistent and security-load-bearing. **Binding decision:**

- The **bootstrap SPKI pin is compiled into the agent binary** (which is Authenticode-signed) — the trust anchor an attacker cannot swap without replacing a signed binary.
- **Rotation pins** are delivered over the already-pinned channel and stored in the **ACL-locked state store** (bbolt), never in `Users`-readable `config.yaml`. `config.yaml` carries only non-security operational settings.
- **Pin rotation/expiry recovery (mandatory to document):** pin a *set* with overlapping validity; deliver new pins ahead of expiry over the trusted channel; the documented last-resort fallback is an installer-driven re-pin (a new signed binary). A pin expiry must **never** be an unrecoverable fleet lockout (see REV-2).

### 0.6 Updater scope cut — honor the PRD's "placeholder logic"

> **§0.6 is the single, authoritative source of truth for updater scope in Sprint 1.** Any statement anywhere else in this document (§1–§5) that implies a second persistent Windows service, an always-on watchdog service, or an updater self-update / two-stage bootstrap in Sprint 1 is **void** and is to be read as corrected by this subsection. Sprint 1 ships exactly **one** Windows Service (`BeyzBackupAgent`); `beyz-backup-updater.exe` is installed as a binary and invoked **on demand**.

PRD req 12 asks for an updater with **placeholder logic**, and req 16 to register **the service** (singular = the agent). The working design inflated this to a second always-on watchdog service + two-stage self-update bootstrap + full health-gated FSM — realistically 2–3 sprints, and the highest-risk code is exactly what the PRD wanted stubbed. **Binding scope decision:**

| Updater capability | Sprint 1 | Deferred (Sprint 8 "Signed Updates") |
|---|---|---|
| Manifest schema + RFC 8785 JCS canonicalization | ✅ real | |
| Ed25519 signature verify — embedded **test** key, key-id + revocation-list aware | ✅ real, **enforcing** (never `return true`) | production HSM / offline signing ceremony |
| BLAKE3 + SHA256 payload hash check | ✅ real | |
| Anti-rollback version-floor check | ✅ real | |
| FSM defined + persisted (`updater_state.json`) | ✅ real | |
| Atomic `MoveFileEx` swap of the **agent** binary + integrity-checked rollback | ✅ real | |
| Health gate via the agent's post-update heartbeat ack | ✅ real (heartbeat is Sprint-1 scope anyway) | |
| Updater **delivery model** | **on-demand / scheduled-task binary** (invoked by the agent or an SCM scheduled task) | persistent **2nd watchdog service** |
| Updater **self-update** (two-stage bootstrap) | **stub** → `MOVEFILE_DELAY_UNTIL_REBOOT` fallback, documented | full two-stage bootstrap helper |
| **Watchdog** (recover a dead agent) | **deferred** — Windows Service recovery actions are the Sprint-1 backstop | persistent watchdog |

**Net:** Sprint 1 ships **one service (`BeyzBackupAgent`)** plus a **real-verification, on-demand updater binary**. The installer **reserves** the second-service registration but does not run a 2nd persistent service. Security stays enforcing (per CLAUDE.md); the cut removes only the watchdog/self-update/long-poll niceties. (This closes former Open Question OQ-16 — see 05-OPEN-QUESTIONS.md.)

### 0.7 Concurrency, single-instance, supply-chain & residency (review-added foundations)

- **Single-instance guard** on the agent (named mutex / lock file) so a console-mode run and the service can't both run and race the bbolt writer. The agent is the **sole bbolt writer**; the updater uses its own `updater_state.json`.
- **Supply-chain of the *first* binary** matters as much as update trust: pin `go.sum`, `GOFLAGS=-mod=readonly`, and run `govulncheck` + `gosec` + `gitleaks` as **required** CI gates; emit an **SBOM** (`cyclonedx-gomod`); vet the third-party dependency set (REV-1).
- **Data residency:** the enrollment/tenant record carries a **`region` / `residency` claim from day one** (hotels/MSPs/multi-location imply it); retrofitting it into issued certs later is exactly the irreversible trap (REV-5).
- **Installer is an attack surface:** ACLs at create-time, `/TOKEN=` written SYSTEM-only and deleted on consume, and anti-rollback applied to the **installer** path too — not only the updater path (REV-4).

### 0.8 What Sprint 1 freezes forever (the irreversible-decision checklist)

Sign-off on §0 means accepting that the first shipped installer bakes these in: `/v1/` path + `X-Agent-Version`/`X-Protocol-Version` headers + `426` floor (ARCH-2); Ed25519 + embedded key-**set** + `key_id` + revocation-list field (UPD-1/2); immutable `tenant_id` (+ MSP `parent_org_id`) and `region` bound in the cert (ARCH-6/REV-5); server-issued opaque `device_id` as primary identity (LIC-2); `recovery_policy` branch in enrollment (§0.3); frozen hash-chained audit-event schema (ARCH-9); OpenAPI spec as source of truth (ARCH-1); anti-rollback semantics in the manifest (UPD-6); compiled-in bootstrap SPKI pin + rotation set (§0.5).


### 0.9 T12 Transport Approval Conditions

S1-T12 (the hardened control-channel HTTP client) is **APPROVED WITH CONDITIONS** after a senior architecture review. The full record is **ADR-005**; the binding conditions are summarized here because they constrain Sprint 2 (server), S1-T13, and S1-T17.

The transport's code is production-grade; the risks are **system-level decisions T12 forces** — chiefly that SPKI pinning targets the server **leaf public key**, coupling the whole fleet to how the SaaS terminates control-channel TLS.

1. **Stable control-channel TLS key (binding).** The Helios SaaS control channel **must** present a **stable, Beyz System-controlled TLS leaf key** (or a small managed key-set explicitly designed for SPKI pinning). **Do not rely on auto-rotating CDN/managed TLS leaf keys** (Cloudflare/ALB managed certs) for the pinned endpoint — a provider key rotation would cause a **fleet lockout**. If a CDN/managed TLS fronts the public app, the **agent control-channel endpoint** must either terminate on a stable Beyz System key or be a **dedicated agent API endpoint** with stable pinning semantics.
2. **Pin rollover = restart in Sprint 1.** Runtime pin reload is **not** required now; the pin set is read at startup (compiled-in bootstrap ∪ ACL-locked state store, §0.5). **Dynamic pin provider / runtime reload is deferred to Sprint 8.** A **fleet pin-rollover runbook must be written and tested before production release** (OQ-26).
3. **Enterprise-network limits (documented, OQ-27):** TLS inspection / SSL interception **breaks pinning by design** → the agent control-channel FQDN must be **bypassed** in the inspection proxy; **NTLM/Kerberos authenticated proxy is not supported** in Sprint 1; **Basic proxy** via system env or explicit URL **is** supported.
4. **T13 integration contract:** `ServerName` from the `api_base_url` host; `TokenProvider` returns an **in-memory cached** token (no DPAPI unwrap per request); **401** and **426** handled by the caller; **no duplicate version-header editors** (T12 injects them); **T12 is control-channel only — never for large backup payloads** (those go through `pkg/storage`).
5. **T17 heartbeat/poll:** cadence **jitter**, honor **Retry-After**, **low retry count** for heartbeat/poll, **circuit-breaker**, and **recovery thundering-herd mitigation**.
6. **Sprint 2 backend:** under overload the SaaS API must prefer **`429 + Retry-After`** over generic **5xx** to avoid the per-call retry (≤5×) **amplifying** load across a large fleet.

**Release blockers** for a production fleet: conditions #1 (stable TLS key) and #2's runbook. For a single controlled pilot (Beyz System operates the SaaS) these are operational pre-conditions, not hard blockers.


## §1. Goals, Scope, Deliverables, Components, Folder Structure, Dependencies

> The subsections below are the detailed design. Where they name a library, endpoint count, state-store location, or Go version that conflicts with §0, **§0 wins** (these were independent design tracks).

This section establishes the Sprint 1 foundation for **Helios**. Sprint 1 ships the **agent foundation only** — no backup/restore/crypto engine — but it makes binding decisions on identity, wire protocol, on-disk format, and trust roots that every later sprint inherits. The risk review (ARCH/SEC/SCALE/BKP/RST/UPD/LIC/STO/GAP) is treated as authoritative: where a "decide-now / impossible-later" item touches a Sprint 1 primitive (enrollment, config schema, wire protocol, manifest envelope, update trust root), this design **reserves the field or fixes the decision now** even though enforcement code lands later.

---

### Goals

Crisp, measurable Sprint 1 exit criteria. Sprint 1 is **Done** when all of the following are objectively true:

| # | Goal | Measurable acceptance |
|---|------|----------------------|
| G1 | **Runnable Windows Service** | `beyz-backup-agent.exe` installs, registers, starts, stops, and survives reboot via Windows SCM; `sc query BeyzBackupAgent` reports `RUNNING`. Linux systemd unit file present and lint-clean (not required to run in CI). |
| G2 | **Operator config** | Agent reads `C:\ProgramData\BeyzBackup\config.yaml`, validates it against a published schema, and fails loud with a non-zero exit and an actionable error on invalid/missing config. |
| G3 | **Structured logging** | All log output is JSON (zerolog), one event per line, to `C:\ProgramData\BeyzBackup\logs\agent.log` with rotation; every record carries `tenant_id`, `device_id`, `event_type`, `ts`, `outcome` (GAP-7/ARCH-9 schema). |
| G4 | **Contract-first protocol** | A versioned **OpenAPI 3.1 spec** for the 4 agent endpoints is committed and is the source of truth (ARCH-1). The agent HTTP client is generated/validated from it and passes contract tests against a **Prism** mock server in CI. |
| G5 | **Enrollment flow (against mock)** | Agent consumes a single-use enrollment token, generates a local keypair + CSR, calls `POST /v1/enroll`, persists the returned `device_id`, signed cert, and signed license blob to the protected state store. Re-running with a consumed token fails (mock enforces single-use). |
| G6 | **Heartbeat sender** | Agent sends `POST /v1/agents/{id}/heartbeat` on a **server-controlled, jittered** interval (SCALE-1), carrying the minimal presence payload (SCALE-2), and applies the `next_poll_seconds` returned by the server. |
| G7 | **Task-poll placeholder** | Agent calls `GET /v1/agents/{id}/tasks`, parses the versioned envelope, logs "no tasks", and handles the reserved `work_available` / rollout fields without erroring on unknown fields (SCALE-9). |
| G8 | **Updater with REAL verification** | `beyz-backup-updater.exe` fetches a signed manifest, **verifies an Ed25519 signature against a public key embedded at build time** (UPD-1, ARCH-4 — *never* a `return true` stub), enforces anti-rollback (UPD-6), verifies BLAKE3 hash of the binary, performs the staged atomic replace + health-gated rollback **state machine** (against a fixture manifest in tests). |
| G9 | **Installer** | Inno Setup script produces a signed-capable installer that lays down both binaries, creates folders with **locked ACLs (SYSTEM + Administrators only)** (SEC-6), registers + starts the service, and accepts the enrollment token via dialog or `/TOKEN=` parameter (excluded from logs). |
| G10 | **Tested & documented** | Unit + integration tests pass in CI (`go test ./...` green); the 6 design records (key-mgmt, enrollment, update-trust, tenancy, manifest-envelope, audit-schema) are committed to `/docs`. |

**Non-goals as metrics:** zero lines of backup/chunking/encryption/restore code; zero real SaaS backend code; zero production signing key material in repo or CI.

---

### Scope

#### IN scope

- **Agent runtime**: Windows Service lifecycle (install/start/stop/uninstall hooks), graceful shutdown, panic recovery, single-instance guard. Linux systemd unit + service abstraction prepared (cross-compiles, not gated in CI).
- **Config subsystem**: `config.yaml` load → schema-validate → typed struct, with **reserved-but-unused** fields for future sprints (crypto, schedule, throttle, storage) so the on-disk format never breaks (GAP-1, GAP-6, RST-1).
- **State store**: separate machine-protected store for `device_id`, agent private key, signed cert, signed license blob — atomic write-temp-rename, restrictive ACL (ARCH-8, SEC-6).
- **Structured logging**: JSON logs + the **security/audit event schema** (hash-chain-ready, fields frozen now) emitted for enrollment and update events (ARCH-9, SEC-7, GAP-7).
- **Wire contract**: OpenAPI 3.1 spec for `POST /v1/enroll`, `POST /v1/agents/{id}/register`*, `POST /v1/agents/{id}/heartbeat`, `GET /v1/agents/{id}/tasks`; version headers; forward-compatible envelopes (ARCH-1/2, SEC-8, SCALE-9, BKP-7).
- **Enrollment client**: keypair + CSR generation, token consumption, cert/license persistence; device fingerprint = SPKI thumbprint + persisted random device GUID (SEC-5, ARCH-7, LIC-2).
- **Heartbeat + task-poll clients**: jitter/backoff, keep-alive reuse, server-directed cadence (SCALE-1/3).
- **HTTP client**: TLS 1.2+/1.3, **SPKI certificate pinning** for the control channel, proxy support, IPv6, retry/backoff with full jitter (SEC-4, GAP-5, GAP-8).
- **Updater**: real Ed25519 manifest verification, BLAKE3 + SHA256 binary hash check, anti-rollback, staged `current/new/backup` layout, health-gated rollback state machine **of the agent binary**, key-id-based multi-key trust set — delivered as an **on-demand binary, NOT a second persistent service** (the persistent watchdog and the updater self-update two-stage bootstrap are **deferred to Sprint 8**, per §0.6) (UPD-1..9).
- **Installer**: Inno Setup script, folder + ACL creation, dual-binary install, service registration/start, token input, uninstall that purges secrets and best-effort de-enroll (GAP-4).
- **Build/test/CI**: Taskfile, cross-compile matrix, `go vet`/`staticcheck`/`golangci-lint`, unit + contract + integration tests, Inno build step.
- **6 design records** (`/docs`): key management, enrollment & identity, update trust & rotation, tenancy & isolation, manifest/format envelope, audit-event schema.

\* `register` is folded into the `enroll` response in this design (see Components) but the path is reserved in the spec for the cert-renewal/re-enroll flow.

#### OUT of scope (explicitly deferred)

| Deferred item | Sprint | What Sprint 1 still does |
|---|---|---|
| Backup engine, chunking (FastCDC), dedup, refcounting | 3, 5 | Freeze manifest envelope + chunk-ID scheme **on paper** (BKP-2/3, STO-5) |
| Compression (ZSTD), encryption (AES-256-GCM), key derivation | 4 | Reserve `wrapped_device_key`, `key_id`, `key_wrap_version` config/enroll fields; write key-mgmt record (GAP-1, BKP-8) |
| Restore engine, integrity/quarantine | 6 | Freeze manifest schema-version + hash-tag format on paper (RST-2/3) |
| Real SaaS backend (FastAPI), DB, Redis, UI | 2 | Prism mock server stands in for the API; OpenAPI spec is the contract |
| Real update **signing infrastructure** (HSM/KMS) + production keys | 8 | Updater verifies against an **embedded test public key**; private key custody documented, not built |
| mTLS, audit aggregation store, ransomware detection | 8, 10 | SPKI pin + client-cert groundwork; local audit schema frozen |
| Storage backends (SMB/SFTP/S3/MinIO) | 7 | `StorageBackend` Go interface + capability flags defined, local no-op stub only (STO-2) |
| Licensing enforcement, billing, MSP UI | 2+ | License **claim set** frozen; signed-blob load/verify path stubbed with test key (LIC-1/4/5) |

---

### Deliverables

Concrete artifacts produced by Sprint 1:

**Binaries**
- `beyz-backup-agent.exe` (Windows, amd64) + `beyz-backup-agent` (Linux amd64/arm64 cross-compiled).
- `beyz-backup-updater.exe` (Windows, amd64) + Linux equivalent.

**Installer**
- `installer/beyz-backup.iss` — Inno Setup script.
- `BeyzBackupSetup-<version>.exe` — built installer artifact (CI output).

**Configuration**
- `configs/config.sample.yaml` — annotated sample with all Sprint 1 keys + reserved future keys.
- `configs/config.schema.json` — JSON Schema used for runtime validation.
- `build/linux/beyz-backup-agent.service` — systemd unit (prep).

**Contracts & specs**
- `api/openapi.yaml` — versioned OpenAPI 3.1 spec (the source of truth, ARCH-1).
- `api/manifest.schema.json` — update-manifest schema (Ed25519-signed, key-id, version floor, rollout fields — UPD-7).
- `docs/format/manifest-envelope.md` — reserved backup-manifest envelope (BKP-3, RST-2/9).

**Design records (one page each, `/docs/design/`)**
- `DR-01-key-management.md` (GAP-1, BKP-8, RST-1, SEC-1)
- `DR-02-enrollment-and-identity.md` (GAP-2, SEC-5, ARCH-7, LIC-2)
- `DR-03-update-trust-and-rotation.md` (UPD-1/2, SEC-3, ARCH-4)
- `DR-04-tenancy-and-isolation.md` (ARCH-6, SCALE-4, STO-4)
- `DR-05-protocol-versioning.md` (ARCH-2, SEC-8, SCALE-9, BKP-7)
- `DR-06-audit-event-schema.md` (ARCH-9, SEC-7, GAP-7)

**Tests**
- Unit tests per package (`go test ./...`).
- Contract tests: agent client ↔ Prism mock from `openapi.yaml`.
- Integration test: updater state machine against fixture manifest (verify→stage→swap→health-gate→rollback).
- Test vectors: Ed25519 signature + BLAKE3 hash fixtures.

**Docs**
- `README.md` (build/run/test), `docs/INSTALL.md`, updated `ARCHITECTURE.md` (expanded component map, ARCH-3), `SECURITY.md` (on-disk secret model, pin rotation), `ROADMAP.md` (status flip).

---

### Components

Go modules/packages, grouped by binary, each with a single responsibility (Clean Architecture: `cmd` = composition root, `internal` = use-cases + adapters, `pkg` = shared kernel). Dependencies point inward; adapters implement interfaces defined by the core. DI is constructor-based (no global singletons except the logger root).

#### Shared kernel (`pkg/` — importable, stable, no business policy)

| Package | Responsibility (one line) |
|---|---|
| `pkg/proto` | Generated request/response types + version constants for the agent↔SaaS wire contract (from `openapi.yaml`); the single source of wire structs. |
| `pkg/wireversion` | Holds `AgentVersion`, `ProtocolVersion`, `MinSupportedProtocol`; helpers to set `X-Agent-Version`/`X-Protocol-Version` headers and detect `426 Upgrade Required` (ARCH-2, SEC-8). |
| `pkg/manifest` | Update-manifest struct, canonical (deterministic) serialization, and schema-version constants — shared by updater and (future) signing tooling (UPD-7). |
| `pkg/hashing` | Algorithm-tagged digest helpers (`blake3:<hex>` / `sha256:<hex>`), dispatch-on-tag verify; BLAKE3 default, SHA256 for the update path (BKP-5, RST-3, UPD-8). |
| `pkg/storage` | `StorageBackend` interface (`Put/Get/Delete/List/Stat`) + `Capabilities()` flags; local no-op stub only this sprint (STO-2). |

#### Agent core (`internal/agent/...`)

| Package | Responsibility |
|---|---|
| `internal/agent/app` | Composition root / DI wiring: builds config → logger → state store → http client → enroll/heartbeat/poll services and starts the run loop. |
| `internal/agent/config` | Loads, validates (JSON Schema), and types `config.yaml`; exposes immutable typed config; rejects unknown-but-required, ignores reserved-unknown (forward-compat). |
| `internal/agent/state` | Machine-protected state store: device GUID, private key, signed cert, signed license blob; atomic write-temp-rename; ACL/DPAPI enforcement boundary (ARCH-8, SEC-6, STO-1). |
| `internal/agent/identity` | Device GUID generation/persistence + fingerprint = SPKI thumbprint ⊕ device GUID; keypair + CSR generation (SEC-5, ARCH-7). |
| `internal/agent/enroll` | Enrollment use-case: consume token → submit CSR → persist `device_id`/cert/license; idempotent re-enroll path; emits audit events (GAP-2, SEC-2). |
| `internal/agent/heartbeat` | Builds minimal presence payload, sends on server-directed jittered cadence, applies `next_poll_seconds`, carries `license_state`/`reported_usage_bytes` reserved fields (SCALE-1/2, LIC-1). |
| `internal/agent/tasks` | Task-poll placeholder: parses versioned task envelope, handles `work_available`/rollout fields, logs and no-ops (SCALE-3, SCALE-9). |
| `internal/agent/license` | Loads + verifies the server-signed license blob against an embedded public key on every start; advisory enforcement only this sprint (LIC-4/5). |
| `internal/agent/service` | OS service lifecycle adapter (start/stop/shutdown/restart) abstracting Windows SCM vs Linux systemd (G1). |
| `internal/agent/audit` | Emits the frozen security-event schema (hash-chain-ready) for enrollment/update/auth events; wraps the logger (ARCH-9, SEC-7, GAP-7). |

#### Transport (`internal/transport/...` — shared by agent + updater)

| Package | Responsibility |
|---|---|
| `internal/transport/httpclient` | Hardened HTTP client: TLS 1.2+/1.3, **SPKI pinning**, keep-alive reuse, proxy, IPv6, exponential backoff + full/decorrelated jitter, version headers (SEC-4, GAP-5/8, SCALE-1/3). |
| `internal/transport/saasclient` | Typed client over `pkg/proto`: `Enroll/Heartbeat/PollTasks`; maps `426`/error envelopes; the only place that knows endpoint paths (ARCH-1). |

#### Updater core (`internal/updater/...`)

| Package | Responsibility |
|---|---|
| `internal/updater/app` | Updater composition root: orchestrates the update state machine for an **on-demand** check/apply invocation (scheduled-task or agent-triggered); runs to completion and exits (no persistent service in Sprint 1, §0.6). |
| `internal/updater/manifestcheck` | Fetches the signed manifest from the pinned endpoint; parses + version-floor/anti-rollback decision (UPD-5/6, SEC-9). |
| `internal/updater/verify` | **Real** Ed25519 multi-key (key-id) signature verification over the canonical manifest, then BLAKE3 hash check of the downloaded binary — verify-then-exec on the same locked handle (UPD-1/2/8, ARCH-4, SEC-3/9). |
| `internal/updater/trust` | Holds the compile-time embedded public key-set + key-ids + (empty) revocation list; the trust anchor (UPD-1/2, ARCH-4). |
| `internal/updater/swap` | Staged `current/new/backup` layout; atomic `MoveFileEx` replace **of the agent binary**; rollback-copy integrity check (UPD-3, SEC-9). Updater self-update (two-stage bootstrap) is **deferred to Sprint 8** (§0.6). |
| `internal/updater/healthgate` | Post-restart health gate: waits for the new agent to self-report "update OK" via heartbeat within timeout; commits or triggers rollback (UPD-4). |

#### Composition roots (`cmd/`)

| Binary | Package | Responsibility |
|---|---|---|
| `beyz-backup-agent` | `cmd/agent` | Parse flags, select service vs console mode, hand off to `internal/agent/app`. |
| `beyz-backup-updater` | `cmd/updater` | Parse flags (`check` / `apply`), hand off to `internal/updater/app`; runs on demand and exits — **not a persistent service in Sprint 1** (§0.6). |

---

### Folder Structure

Single Go module monorepo. Tree with annotations (no source shown — responsibilities only):

```text
beyz-backup/
├── go.mod                          # module github.com/beyzbackup/beyz-backup
├── go.sum
├── Taskfile.yml                    # task runner: build, test, lint, dist, installer
├── README.md                       # build / run / test entry point
├── .golangci.yml                   # lint config (golangci-lint)
├── .github/
│   └── workflows/
│       └── ci.yml                  # vet + lint + unit + contract + cross-compile + inno build
│
├── api/                            # CONTRACTS = source of truth (ARCH-1)
│   ├── openapi.yaml                # OpenAPI 3.1: enroll / register / heartbeat / tasks
│   ├── manifest.schema.json        # update-manifest JSON Schema (signed, key-id, floor)
│   └── prism/                      # mock-server config + example responses for CI
│
├── cmd/                            # composition roots only (thin main packages)
│   ├── agent/                      # → beyz-backup-agent[.exe]
│   └── updater/                    # → beyz-backup-updater[.exe]
│
├── internal/                      # private application code (not importable externally)
│   ├── agent/
│   │   ├── app/                    # DI wiring + run loop
│   │   ├── config/                 # config.yaml load + JSON-Schema validate
│   │   ├── state/                  # protected state store (GUID/key/cert/license)
│   │   ├── identity/               # device GUID + keypair + CSR + fingerprint
│   │   ├── enroll/                 # enrollment use-case
│   │   ├── heartbeat/              # presence sender (jitter, server cadence)
│   │   ├── tasks/                  # task-poll placeholder
│   │   ├── license/               # signed-license load + verify (advisory)
│   │   ├── service/                # Windows SCM / Linux systemd lifecycle adapter
│   │   └── audit/                  # security-event emitter (frozen schema)
│   ├── updater/
│   │   ├── app/                    # update state machine orchestrator
│   │   ├── manifestcheck/          # fetch + anti-rollback decision
│   │   ├── verify/                 # Ed25519 sig + BLAKE3 hash (REAL)
│   │   ├── trust/                  # embedded public key-set + key-ids + revocation
│   │   ├── swap/                   # staged current/new/backup atomic replace
│   │   └── healthgate/             # post-update health gate + rollback trigger
│   └── transport/
│       ├── httpclient/             # TLS+pin, jitter/backoff, proxy, version headers
│       └── saasclient/             # typed client over pkg/proto
│
├── pkg/                            # shared, importable, stable kernel
│   ├── proto/                      # generated wire types + version constants
│   ├── wireversion/                # agent/protocol version + 426 handling
│   ├── manifest/                   # update-manifest struct + canonical serialization
│   ├── hashing/                    # algo-tagged digests (blake3:/sha256:)
│   └── storage/                    # StorageBackend interface + Capabilities (stub)
│
├── configs/
│   ├── config.sample.yaml          # annotated sample (Sprint 1 + reserved keys)
│   └── config.schema.json          # runtime validation schema
│
├── build/
│   ├── windows/                    # versioninfo, manifest, icon, signing notes
│   ├── linux/
│   │   └── beyz-backup-agent.service   # systemd unit (prep)
│   └── keys/
│       └── update_pub_test.pem     # EMBEDDED TEST public key (no private key, ever)
│
├── installer/
│   ├── beyz-backup.iss             # Inno Setup script (ACLs, service, /TOKEN=)
│   └── scripts/                    # pre/post-install helpers (ACL set, de-enroll)
│
├── docs/
│   ├── ARCHITECTURE.md             # expanded component map (ARCH-3)
│   ├── SECURITY.md                 # on-disk secret model, pin rotation
│   ├── INSTALL.md
│   ├── design/                     # the 6 one-page decision records (DR-01..06)
│   └── format/
│       └── manifest-envelope.md    # reserved backup-manifest envelope (BKP-3/RST-2)
│
└── test/
    ├── contract/                   # agent client ↔ Prism mock tests
    ├── integration/                # updater state-machine end-to-end (fixtures)
    └── fixtures/                    # signed manifest, ed25519/blake3 test vectors
```

---

### Dependencies

Each choice states **a recommendation with rationale**, not a menu.

#### Go libraries

| Concern | Recommendation | Rationale |
|---|---|---|
| **Service management** | `github.com/kardianos/service` | Single API over Windows SCM **and** Linux systemd/launchd, exactly matching G1 (Windows now, systemd "prepped"). Avoids hand-rolling `golang.org/x/sys/windows/svc` + separate Linux glue this early. Keep `internal/agent/service` as a thin interface so we can drop to raw `x/sys/windows/svc` later if we need finer SCM control (recovery actions, delayed-auto-start) — Inno handles those settings for Sprint 1. |
| **Logging** | `github.com/rs/zerolog` | Zero-alloc structured JSON out of the box, leveled, with easy fixed-field context (`tenant_id`/`device_id`) — ideal for the frozen audit schema. Chosen over stdlib `slog`: zerolog's JSON encoder and field ergonomics are more mature, and the audit hash-chain wrapper is simpler to layer on. (`slog` remains a viable fallback; we isolate logging behind `internal/agent/audit` so swapping is cheap.) |
| **Config** | `github.com/knadh/koanf` + `gopkg.in/yaml.v3` + `github.com/santhosh-tekuri/jsonschema/v6` | koanf is lighter and less opinionated than Viper (no global state, no pulled-in remote/etcd backends we don't want in a security-sensitive agent). yaml.v3 for parsing; jsonschema validates `config.yaml` against `config.schema.json` so invalid config fails loud (G2). Viper rejected: heavyweight, global, larger dependency surface. |
| **HTTP client** | stdlib `net/http` + `golang.org/x/net/http2` | Full control over `tls.Config` is mandatory for **SPKI pinning** and TLS 1.2+/1.3 enforcement (SEC-4); stdlib gives keep-alive reuse, proxy (`http.ProxyFromEnvironment` + explicit override), and IPv6 for free. No third-party HTTP framework — fewer trust dependencies on the control channel. |
| **Backoff/jitter** | `github.com/cenkalti/backoff/v4` | Battle-tested exponential backoff with full jitter and max-interval caps; satisfies SCALE-1 decorrelated-jitter reconnect without bespoke timer math. |
| **Client codegen** | `github.com/oapi-codegen/oapi-codegen` (types + client) | Generates `pkg/proto` structs and the typed client **from `openapi.yaml`**, enforcing ARCH-1's "spec is the source of truth, not the Go code." Regeneration is a CI step; drift fails the build. |
| **Contract/mock server** | **Prism** (`@stoplight/prism-cli`, via npx in CI) | Spins a mock SaaS from `openapi.yaml` so the agent's enroll/heartbeat/poll paths run against the same artifact Sprint 2 implements (ARCH-1). Schemathesis optional later for fuzzing. |
| **Crypto — signature verify** | stdlib `crypto/ed25519`, `crypto/x509`, `crypto/sha256` + `github.com/zeebo/blake3` | **Ed25519** for update signatures (small, fast, no padding pitfalls — UPD-1); `x509` for CSR generation + cert handling (SEC-5); BLAKE3 (`zeebo/blake3`, well-maintained pure-Go + asm) as the default content hash, SHA256 reserved for the update-binary path per CLAUDE.md (BKP-5/UPD-8). No third-party crypto for the actual primitives. |
| **Windows secret protection** | `github.com/billgraziano/dpapi` (or thin `x/sys` wrapper) | DPAPI machine-scope to encrypt the private key/license at rest under SYSTEM, behind the `internal/agent/state` interface (SEC-6, ARCH-8). Isolated so the Linux path (root-only `0600`, future TPM/keyring) implements the same interface. |
| **UUID (device GUID)** | `github.com/google/uuid` | Persisted random v4 device GUID surviving reinstall (ARCH-7, LIC-2). Trivial, ubiquitous, stable. |
| **Testing** | stdlib `testing` + `github.com/stretchr/testify` | testify for readable assertions/mocks on the use-case interfaces; keeps DI-based unit tests clean. |

#### Build toolchain

| Concern | Recommendation | Rationale |
|---|---|---|
| **Go version** | **Go 1.23.x** (pinned in `go.mod` + CI) | Current stable with mature `slog`, `crypto/ed25519`, and toolchain version pinning. One minor floor avoids the mixed-toolchain drift the risk review warns about for the fleet. |
| **Task runner** | **Taskfile** (`go-task/task`) | Cross-platform (Windows + Linux dev) YAML task runner; cleaner than a POSIX Makefile on Windows where the installer is built. Targets: `build`, `cross`, `lint`, `test`, `contract`, `dist`, `installer`. |
| **Installer** | **Inno Setup 6** | Mandated by CLAUDE.md; Pascal-script install with folder creation, **ACL lock-down** (icacls in a post-install step), service registration/start, and `/TOKEN=` parameter with log exclusion (G9, SEC-6). |
| **Lint/static** | `golangci-lint` (incl. `staticcheck`, `govet`, `gosec`) | `gosec` is non-negotiable given the security-first mandate; runs in CI as a merge gate. |
| **CI** | **GitHub Actions** | Matrix cross-compile (windows/amd64, linux/amd64+arm64), runs vet/lint/unit/contract tests, regenerates from OpenAPI to detect drift, and builds the Inno installer on a Windows runner. Signing of the installer/binaries is a documented, gated step (real keys never in CI per UPD-1/SEC-3 — Sprint 1 uses a test signing key only). |

#### Cross-cutting decisions locked here (consumed by all components)

- **Versioning**: every path is `/v1/...`; every request carries `X-Agent-Version` + `X-Protocol-Version`; server may return `426`; every wire message and manifest carries a `schema_version` and ignores unknown fields (ARCH-2, SEC-8, SCALE-9, BKP-7).
- **Identity**: `device_id` is **server-issued** at enrollment and bound into the cert; fingerprint = SPKI thumbprint ⊕ persisted random device GUID (never raw hardware serials as the primary key) (SEC-5, LIC-2, ARCH-7).
- **Tenancy**: enrollment token is tenant-scoped; immutable `tenant_id` (+ `parent_org_id` for MSP) embedded in the issued cert; every request authorized against that binding (ARCH-6, LIC-6, SCALE-4).
- **Update trust**: Ed25519 over the canonical manifest, **embedded public key-set with key-ids + empty revocation list** from day one; private-key custody documented as offline/HSM, never in repo/CI (UPD-1/2, SEC-3, ARCH-4).
- **Secrets on disk**: non-sensitive settings in plaintext `config.yaml`; all secret material (key/cert/license/device GUID) in the DPAPI-protected state store with SYSTEM+Administrators-only ACLs; CLAUDE.md's "secrets in env" rule is reconciled to mean **"no secrets in source/config"** (ARCH-8, SEC-6).

**Relevant file paths (all proposed, to be created in Sprint 1):**
`/Users/coosef/Documents/cloude/backup/api/openapi.yaml`, `/Users/coosef/Documents/cloude/backup/api/manifest.schema.json`, `/Users/coosef/Documents/cloude/backup/configs/config.sample.yaml`, `/Users/coosef/Documents/cloude/backup/configs/config.schema.json`, `/Users/coosef/Documents/cloude/backup/installer/beyz-backup.iss`, `/Users/coosef/Documents/cloude/backup/docs/design/DR-01..06`, `/Users/coosef/Documents/cloude/backup/build/keys/update_pub_test.pem`.

---

## §2. Enrollment, Heartbeat & Task-Polling Flows + API Requirements

> The subsections below are the detailed design. Where they name a library, endpoint count, state-store location, or Go version that conflicts with §0, **§0 wins** (these were independent design tracks).

This section defines the **agent ↔ SaaS control-plane contract** for Sprint 1. The SaaS backend itself is Sprint 2, so Sprint 1 ships: (a) a **versioned OpenAPI 3.1 spec committed to the repo as the source of truth** (`/api/openapi-control-v1.yaml`), (b) a Go agent client **generated from that spec**, and (c) a **Prism/Schemathesis contract-test server** the agent validates against. No agent HTTP code is written against hand-rolled assumptions — every request is shaped by the spec (mitigates **ARCH-1**, **GAP-5**).

The contract is frozen now because auto-update guarantees a mixed-version fleet later. Every primitive that licensing, key-management, multi-tenancy, and signed-updates will bind to is reserved in the wire format from the first commit (**ARCH-2, ARCH-5, ARCH-6, SEC-1, SEC-8, LIC-1, BKP-7, RST-1, UPD-5**).

---

### 1. Cross-Cutting Protocol Decisions (apply to every endpoint)

| Decision | Choice | Rationale / Risk |
|---|---|---|
| Versioning | Path-prefixed `/api/v1/...` **and** `X-Protocol-Version: 1` header on every request | Path version for routing; header for fine-grained negotiation. **ARCH-2, SEC-8, SCALE-9** |
| Agent version reporting | `X-Agent-Version: 1.4.0` (semver) header on every request | Server records and gates fleet. **ARCH-2, BKP-7** |
| Min-version enforcement | Server returns **`426 Upgrade Required`** + `min_supported_version` body when agent < floor | One-line-now, impossible-later. Floor raised after fleet auto-updates. **SEC-8, ARCH-2** |
| Forward compatibility | Every message is a JSON **envelope**; agent and server **ignore unknown fields**; never error on extra keys | Lets Sprint 2–8 add fields without a wire break. **SCALE-9, BKP-7** |
| Transport | HTTPS, **TLS 1.3 (1.2 floor)**, **SPKI public-key pin** of the SaaS endpoint compiled into the agent; system-CA-only trust is **rejected** for the control channel | Plain TLS+token is acceptable for Sprint 1 *only* with pinning. **SEC-4** |
| Connection reuse | Mandatory HTTP keep-alive / connection pooling in the agent client (`net/http` with a shared `*http.Client`, `MaxIdleConnsPerHost` tuned) | Amortizes future mTLS handshake across polls. **SCALE-3** |
| Auth (enroll) | One-time **enrollment token** in `Authorization: Bearer <token>` | **SEC-2, GAP-2** |
| Auth (all other) | **Agent client certificate** issued at enrollment, presented as a bearer credential header `Authorization: Bearer <agent_session_token>` in Sprint 1; **groundwork to promote to true mTLS in Sprint 8 without changing enrollment format** | **SEC-4, SEC-5, GAP-5** |
| Idempotency | Mutating calls accept `Idempotency-Key: <uuid>` header; server dedups for 24h | Safe retries through outages |
| Time / clock skew | Server includes `server_time` (RFC3339 UTC) in every response; agent logs/heartbeats a skew warning if local clock differs > 300s; **security decisions use server time** | **GAP-5** |
| Error body | Uniform RFC 9457 `application/problem+json` (`type`, `title`, `status`, `detail`, `instance`, `code`) | Machine-parseable error semantics |
| Proxy / egress | Agent honors system proxy + explicit `network.proxy` config; fixed egress FQDN list published for firewall allow-listing | **GAP-8** |

**Reserved-for-later fields are present from day one** (empty/null in Sprint 1) so the format never breaks:
- Key material: `wrapped_device_key`, `key_wrap_version`, `key_id`, `kdf_params` (**SEC-1, BKP-8, RST-1, ARCH-5**)
- Tenancy: `tenant_id`, `parent_account_id` (**ARCH-6, LIC-6, SCALE-4**)
- Licensing: `license_blob`, `license_state`, `reported_usage_bytes` (**LIC-1, LIC-3, LIC-4**)
- Capability/format: `supported_format_versions { manifest, chunk, crypto_envelope }` (**BKP-7, RST-9**)
- Cadence control: `next_poll_seconds`, `next_heartbeat_seconds`, `poll_jitter_pct` (**SCALE-1**)

---

### 2. API Requirements

Base URL: `https://api.beyzbackup.com/api/v1`. **All five endpoints are MOCKED in Sprint 1** via the Prism server generated from the OpenAPI spec; the real implementation lands in Sprint 2. The agent code is production-real against the mock.

#### 2.1 Endpoint summary

| # | Endpoint | Method | Path | Auth | Idempotent | Sprint 1 status |
|---|---|---|---|---|---|---|
| 1 | Enroll | `POST` | `/enroll` | Bearer **enrollment token** | Yes (token single-use; replay → 409) | Mocked |
| 2 | Register | `POST` | `/agents/{agent_id}/register` | Bearer **agent credential** | Yes (Idempotency-Key) | Mocked |
| 3 | Heartbeat | `POST` | `/agents/{agent_id}/heartbeat` | Bearer **agent credential** | Yes (stateless presence) | Mocked |
| 4 | Poll tasks | `GET` | `/agents/{agent_id}/tasks` | Bearer **agent credential** | Yes (safe, read) | Mocked (returns empty) |
| 5 | Ack task | `POST` | `/agents/{agent_id}/tasks/{task_id}/ack` | Bearer **agent credential** | Yes (Idempotency-Key) | Mocked |
| 6 | Report status | `POST` | `/agents/{agent_id}/tasks/{task_id}/status` | Bearer **agent credential** | Yes (last-write-wins) | Mocked |

> Enroll and Register are split deliberately: enroll consumes the single-use token and returns the credential; register submits the CSR + device facts under the new credential. This keeps token consumption atomic and separate from the heavier device-fact payload (**SEC-2, SEC-5**).

#### 2.2 `POST /enroll` — consume token, issue credential

**Headers:** `Authorization: Bearer <enrollment_token>`, `X-Agent-Version`, `X-Protocol-Version`, `Idempotency-Key`

**Request body:**
```json
{
  "protocol_version": 1,
  "device_guid": "9d8f...random-uuid-v4",      // agent-generated, persisted, survives reinstall (ARCH-7)
  "fingerprint": {
    "machine_guid": "windows-MachineGuid",      // advisory, clone-detection only (LIC-2)
    "primary_disk_serial": "S3EUNX0M...",        // advisory
    "os": "windows", "os_version": "10.0.19045",
    "hostname": "HOTEL-FRONT-01"
  },
  "csr_pem": "-----BEGIN CERTIFICATE REQUEST-----...",  // agent-generated keypair, CSR (SEC-5)
  "supported_format_versions": { "manifest": 1, "chunk": 1, "crypto_envelope": 1 },
  "agent_version": "1.0.0"
}
```
The token is **never** placed in a URL or query string; preferred input is stdin/secure-file/installer dialog, not a plaintext CLI arg (**SEC-2**).

**Response `201 Created`:**
```json
{
  "agent_id": "agt_01HXYZ...",                 // server-issued opaque device id (LIC-2)
  "tenant_id": "tnt_01ABC...",                 // immutable, bound into cert (ARCH-6)
  "parent_account_id": "msp_01QRS...",         // MSP hierarchy, nullable (LIC-6)
  "agent_certificate_pem": "-----BEGIN CERTIFICATE-----...",  // SPKI thumbprint = identity (SEC-5)
  "ca_chain_pem": "...",
  "agent_session_token": "ast_...",            // bearer credential for Sprint 1 calls
  "endpoints": { "heartbeat": "...", "tasks": "...", "register": "..." },
  "cadence": { "next_heartbeat_seconds": 60, "poll_jitter_pct": 20 },   // server-controlled (SCALE-1)
  "license_blob": null,                        // reserved, signed offline license later (LIC-1/4)
  "key_wrap_version": null, "key_id": null,    // reserved key material (SEC-1, BKP-8)
  "cert_not_after": "2026-07-08T00:00:00Z",    // finite cert lifetime; renewal = liveness (LIC-7)
  "server_time": "2026-06-08T12:00:00Z"
}
```

**Status codes & semantics:**

| Code | Meaning | Agent action |
|---|---|---|
| `201` | Token consumed, credential issued | Persist credential atomically, go ENROLLED |
| `401` | Token invalid/expired (TTL 15–60 min) | Fatal; surface to installer; do not retry blindly |
| `409` | **Token already consumed (replay)** | Fatal; log security event; **do not** silently re-enroll (**SEC-2**) |
| `422` | Malformed CSR/fingerprint | Fatal; log |
| `426` | Agent below min version | Fatal; instruct update |
| `429` | Rate-limited / endpoint locked | Backoff per `Retry-After` (**SEC-2** brute-force lock) |
| `5xx` | Server error | Retry with exponential backoff + full jitter |

Single-use is enforced by **atomic server-side row-state consumption** (DB transition `unused → consumed`), never client trust. Every attempt (success/fail) is logged as a security event (**SEC-2, ARCH-9, LIC-5**).

#### 2.3 `POST /agents/{agent_id}/register`
Submits any deferred device facts and confirms credential round-trip. In Sprint 1 the mock returns `200` and echoes the recorded `device_id`. Carries the same `supported_format_versions` and reserved key/license fields. Idempotent via `Idempotency-Key`.

#### 2.4 `POST /agents/{agent_id}/heartbeat` — minimal presence + health

**Request body (deliberately minimal — never touches Postgres on the hot path, SCALE-2):**
```json
{
  "protocol_version": 1,
  "agent_version": "1.4.0",
  "status": "idle",                  // enum: idle|backing_up|restoring|updating|error
  "health": {                        // GAP-9 fleet observability
    "service_state": "running",
    "last_error": null,
    "disk_free_pct": 42,
    "connectivity": "ok"
  },
  "reported_usage_bytes": null,      // reserved, advisory metering (LIC-3)
  "license_state": null,             // reserved (LIC-1)
  "update_result": null              // reserved: post-update "update OK" ack (UPD-4)
}
```

**Response `200 OK`:**
```json
{
  "cadence": { "next_heartbeat_seconds": 60, "poll_jitter_pct": 20 },  // server-tuned (SCALE-1)
  "work_available": false,           // cheap migration path to long-poll/push (SCALE-3)
  "hold_seconds": 0,                 // long-poll hint, 0 in Sprint 1 (SCALE-3)
  "license_blob": null,              // refreshed each heartbeat later (LIC-4)
  "min_supported_version": "1.0.0",
  "server_time": "2026-06-08T12:01:00Z"
}
```
Presence is stored **in Redis** (key per agent with TTL, or sorted-set by last-seen); only `online↔offline` transitions flush to Postgres — designed now, even though Redis writes are Sprint 2 (**SCALE-2**). `update_result` wires the heartbeat ack into the update success signal so rollback can trigger on a missed ack (**UPD-4**).

**Status codes:** `200` ok · `401` credential invalid/revoked → re-enroll (**LIC-7**) · `426` upgrade required · `429` backoff · `5xx` backoff.

#### 2.5 `GET /agents/{agent_id}/tasks` — poll (returns empty in Sprint 1)

Query: `?max=10`. **Response `200`:**
```json
{
  "tasks": [],                       // empty placeholder in Sprint 1
  "cadence": { "next_poll_seconds": 60, "poll_jitter_pct": 20 },
  "server_time": "2026-06-08T12:02:00Z"
}
```
Placeholder task envelope (frozen shape, not dispatched in Sprint 1):
```json
{
  "task_id": "tsk_01...",
  "type": "noop",                    // Sprint 1 enum: noop | config_refresh | update_check
  "schema_version": 1,
  "lease_seconds": 300,              // visibility timeout (lease/ack model below)
  "sequence": 42,                    // monotonic ordering within an agent
  "payload": {},
  "created_at": "..."
}
```
`update_check` payloads carry server-directed rollout fields (`target_version`, `rollout_cohort_pct`, `update_allowed` kill-switch) so the agent only updates when told — reserved now to avoid a wire break (**UPD-5**).

#### 2.6 `POST .../tasks/{task_id}/ack` and `.../tasks/{task_id}/status`
- **Ack** claims/confirms receipt; idempotent; re-ack of an already-acked task → `200` (not error).
- **Status** reports `accepted | in_progress | succeeded | failed | rolled_back` with optional `detail`/`progress_pct`; last-write-wins; emitted through the audit schema (**ARCH-9, RST-7**).

---

### 3. Enrollment Flow

**State machine:** `UNENROLLED → ENROLLING → ENROLLED → (DEGRADED on outage) → DECOMMISSIONED`. State is persisted; the agent boots into the correct state after a crash/restart.

```
Installer/Operator           Agent (beyz-backup-agent.exe)            SaaS (mock in S1)
─────────────────            ────────────────────────────            ─────────────────
 token via dialog/  ───────►  1. read/generate device_guid
 stdin/secure file              (persist to state store)
                              2. generate keypair locally,
                                 build CSR (SEC-5)
                              3. POST /enroll
                                 Bearer <token> + CSR + fingerprint  ──►  4. atomic token
                                                                            consume (unused→
                                                                            consumed); bind
                                                                            tenant_id; sign cert
                              5. 201: agent_id, cert, session token  ◄──    return credential
                              6. atomic persist (write-temp→rename)
                                 of cert+key+agent_id (DPAPI/ACL)
                              7. POST /register (confirm)            ──►   8. 200 echo
                              9. state → ENROLLED; emit audit event
                             10. start heartbeat + poll loops
```

**Key decisions & risk coverage:**

- **Device identity (ARCH-7, LIC-2, SEC-5):** A persisted, agent-generated **random `device_guid` (UUIDv4)** stored under `C:\ProgramData\BeyzBackup\state\` survives app reinstall. The **authoritative identity is the server-issued `agent_id` + the cert's SPKI thumbprint**, *not* a client-computed hardware hash. Hardware signals (`machine_guid`, disk serial) are reported **advisory-only** for clone-detection heuristics. VM-clone handling: a cloned `config/state` whose cert/`device_guid` doesn't match the server-side device record is detectable server-side and forces re-enrollment.
- **Token replay protection (SEC-2):** Single-use enforced by **atomic server-side state transition**, short TTL (15–60 min), tenant-bound, registration endpoint rate-limited and lockable. Replay → `409`, logged as a security event. Token never logged, never in URL/CLI-arg-by-default.
- **Idempotent re-enrollment / device replacement (ARCH-7, LIC-7, GAP-4):** A separate **re-enrollment token** reuses the same license seat and `device_guid`, issuing a fresh cert. Cert has a **finite lifetime** (`cert_not_after`); renewal happens via heartbeat liveness, so revocation = stop renewing (**LIC-7**).
- **Secure persistence (SEC-6, ARCH-8, STO-1):** Credential (cert + private key + `agent_id` + `device_guid`) is written to a **machine-protected state store** separate from operator-editable `config.yaml`, secured via **Windows DPAPI machine-scope** (Linux: root-only `0600`, future TPM), with **ACLs locked to SYSTEM + Administrators** (installer sets these at folder creation, PRD req 15). All state writes are **atomic (write-temp → fsync → rename)** for crash safety. Secrets are a distinct class, never logged, never in plaintext YAML.
- **CLAUDE.md reconciliation (ARCH-8):** "Always use env vars for sensitive config" is read as **"no secrets in source or committed config"** — runtime secrets live in the DPAPI-protected state store, not env vars and not `config.yaml`.

**On-disk layout (Sprint 1):**
```
C:\ProgramData\BeyzBackup\           (ACL: SYSTEM + Administrators only)
├─ config.yaml                       operator-editable, NON-secret only
├─ state\                            machine-protected (DPAPI), not operator-edited
│  ├─ device_guid                    persisted UUIDv4 (survives reinstall)
│  ├─ agent.crt  agent.key           credential (key DPAPI-wrapped)
│  ├─ agent_id                       server-issued id
│  ├─ enrollment.state               UNENROLLED|ENROLLED|...
│  └─ license.blob                   reserved (signed offline license, LIC-4)
├─ logs\                             structured logs + hash-chained audit (SEC-7)
└─ update\  {current, new, backup}   updater staging dirs (UPD-3)
```

**`config.yaml` (non-secret) relevant keys:**
```yaml
api:
  base_url: https://api.beyzbackup.com/api/v1
  spki_pin: "sha256/AAAA...="          # SEC-4 cert pin
  protocol_version: 1
enrollment:
  token_source: dialog                  # dialog | secure_file | stdin (SEC-2)
heartbeat:
  # interval is SERVER-controlled; this is only a fallback floor
  fallback_interval_seconds: 60
network:
  proxy: ""                             # GAP-8
# reserved placeholders so on-disk format never breaks (SEC-1/BKP-8/RST-1)
crypto:
  wrapped_device_key: null
  key_wrap_version: null
  key_id: null
  hash_algo: "blake3"                   # RST-3 tagged default; SHA256 = update path only
```

---

### 4. Heartbeat Flow

**Cadence (SCALE-1):** The interval is **server-controlled** — returned in `cadence.next_heartbeat_seconds` from enroll/heartbeat responses, **never hardcoded**. The agent applies **mandatory randomized jitter** (`± poll_jitter_pct`, default 20%) so a fleet never beats in unison. `config.yaml` holds only a fallback floor used before the first server response.

```
ENROLLED agent loop:
  ┌─────────────────────────────────────────────────────────┐
  │ sleep(next_hb ± jitter%)                                 │
  │ POST /heartbeat  {version,status,health}                 │
  │   ├─ 200 → update cadence; if work_available → poll soon │
  │   ├─ 401 → state DEGRADED → trigger re-enroll (LIC-7)    │
  │   ├─ 426 → state DEGRADED → trigger updater              │
  │   └─ 5xx/network → backoff (decorrelated jitter)         │
  └─────────────────────────────────────────────────────────┘
```

- **Payload (SCALE-2, GAP-9):** Minimal and **separate from the heavier task-poll response** — fingerprint/`agent_id` (in path), version, status enum, health block. This is engineered so presence updates **never touch Postgres**; presence lives in Redis with a TTL, transitions flush to Postgres. Health fields give the SaaS a real signal (version, last-error, service state, disk, connectivity), not bare liveness.
- **Offline / failure handling (SCALE-1, GAP-8):** **Exponential backoff with full jitter** on errors; **decorrelated jitter** on reconnect after the SaaS is unreachable, capped at a max interval. The agent **continues local operation and queues heartbeats** while degraded; it does not hard-fail.
- **Server-side liveness:** Redis key TTL ≈ `2.5 × next_heartbeat_seconds`; expiry marks the agent offline (transition flushed to Postgres). Cadence is tunable centrally without shipping a new agent.
- **Update ack channel (UPD-4):** After **an agent update**, the new agent's **first heartbeat carries `update_result: "ok"`** within a bounded timeout (e.g., 90s). The on-demand updater (still resident for the health-gate window) treats a missed ack as the **rollback trigger** — the heartbeat sender is already Sprint 1 scope, so this is designed in now.
- **Migration path (SCALE-3):** `work_available` + `hold_seconds` are in the response shape from day one, enabling a no-wire-break upgrade to long-polling or a WebSocket/SSE push channel later. HTTP keep-alive is mandated so a future mTLS handshake is amortized.

---

### 5. Task Polling Flow

**Model decision — RECOMMENDED: server-cadence short-poll with a built-in long-poll upgrade path.**

| Option | Verdict |
|---|---|
| Fixed-interval short poll | Rejected alone — wasteful, thundering-herd risk |
| **Server-cadence short poll + `work_available` flag + reserved `hold_seconds`** | **Chosen for Sprint 1** — simplest correct thing; server controls cadence; cheap migration to long-poll/push without a wire break |
| Long-poll / WebSocket now | Deferred — protocol *designed* for it now (`hold_seconds`), *implemented* later (**SCALE-3**) |

The agent polls `GET /tasks` on a **server-supplied, jittered cadence** (`next_poll_seconds ± poll_jitter_pct`), and polls **immediately** when a heartbeat returns `work_available: true`. In Sprint 1 the mock always returns `tasks: []`.

**Lease / ack model (designed now, exercised against the mock):**

```
poll → server returns task with lease_seconds + monotonic sequence
   │
   ▼
ack (claim)  ── POST /tasks/{id}/ack  (Idempotency-Key) ──►  lease starts/refreshes
   │
   ▼
execute placeholder (noop / config_refresh / update_check)
   │
   ├─ heartbeat-piggybacked lease renewal while running (status: in_progress)
   ▼
status ── POST /tasks/{id}/status {succeeded|failed|rolled_back} ──► task closed
   │
   └─ if agent dies → lease expires → task redelivered to the same agent on next poll
```

- **Lease (visibility timeout):** A claimed task is invisible for `lease_seconds`; if the agent crashes before reporting terminal status, the lease expires and the task is **redelivered** — at-least-once delivery. Backed by **Redis** (Sprint 2), designed now.
- **Dedup / idempotency:** Tasks carry a stable `task_id`; ack and status are **idempotent** (Idempotency-Key + last-write-wins). The agent keeps a small **seen-task set** so a redelivered already-completed task is a no-op. This makes at-least-once safe.
- **Ordering:** Per-agent **monotonic `sequence`** field; the agent processes in `sequence` order and ignores out-of-order stragglers. Sprint 1 has no real tasks, but the ordering contract is frozen.
- **Placeholder task types (Sprint 1):** `noop` (smoke-test the lease/ack/status round-trip against the mock), `config_refresh` (re-pull non-secret config), `update_check` (server-directed update with reserved `target_version` / `rollout_cohort_pct` / `update_allowed` kill-switch — **UPD-5**). Backup/restore task types are intentionally absent until Sprint 3+.
- **Audit (ARCH-9, RST-7, GAP-7):** Task lifecycle events (`accepted/in_progress/succeeded/failed/rolled_back`) are emitted through the **minimal structured audit schema** defined in Sprint 1 — `{event_type, tenant_id, device_id, task_id, sequence, timestamp, actor, outcome}` — with **per-record hash-chaining** for tamper-evidence and **near-real-time shipping of security events to the SaaS** so the authoritative copy is off the endpoint. Sprint 8 adds transport/aggregation only; the **event shape is frozen now**.

---

### 6. Sprint 1 Deliverables for This Section

1. **`/api/openapi-control-v1.yaml`** — OpenAPI 3.1 spec for the six endpoints above, committed as the **source of truth** (Go client generated from it via `oapi-codegen`; mitigates ARCH-1).
2. **Contract-test harness** — Prism mock server + Schemathesis property tests in CI so the agent placeholder validates against the artifact Sprint 2 will implement.
3. **Generated Go client** consumed behind a clean interface; production-real (TLS pinning, backoff, idempotency, version headers) — no `return true` security stubs (CLAUDE.md "no placeholder security").
4. **Audit-event schema** (frozen) and **on-disk state/config layout** above.

**Relevant artifact paths:**
- `/Users/coosef/Documents/cloude/backup/api/openapi-control-v1.yaml` (to be authored — source of truth)
- `/Users/coosef/Documents/cloude/backup/docs/ARCHITECTURE.md` (must add Redis presence, Postgres transition-flush, tenancy isolation, chunk-key scheme — ARCH-3/SCALE-2/SCALE-4)
- `/Users/coosef/Documents/cloude/backup/docs/SECURITY.md` (must add token lifecycle, SPKI pin rotation, DPAPI/ACL secret model, audit hash-chain — SEC-2/4/6/7)
- Agent state store: `C:\ProgramData\BeyzBackup\state\` · config: `C:\ProgramData\BeyzBackup\config.yaml`

---

## §3. Database, Configuration & Logging Design

> The subsections below are the detailed design. Where they name a library, endpoint count, state-store location, or Go version that conflicts with §0, **§0 wins** (these were independent design tracks).

> **Scope note (Sprint 1):** The agent ships *before* the SaaS exists (ARCH-1). Therefore everything below is engineered so that (a) the **agent-side on-disk format is the frozen contract** — no breaking change permitted in Sprint 2–8 — and (b) the **server-side tables are a forward declaration** that unblocks Sprint 2 without being implemented now. All wire/disk schemas carry explicit version integers per SEC-8 / SCALE-9 / BKP-7. Secrets are governed by CLAUDE.md §27–31, reinterpreted per ARCH-8 as *"no secrets in source or plaintext config"* — **not** "all secrets in env vars."

---

### Database Requirements

#### 1. The SaaS PostgreSQL schema is **Sprint 2** — not built here

No PostgreSQL, no Redis, no FastAPI persistence is written in Sprint 1. The agent talks only to a **contract-test mock** (Prism/Schemathesis driven by the committed OpenAPI 3 spec, per ARCH-1). What Sprint 1 *does* freeze is (a) the agent's local persistence format and (b) the table shapes the wire contract implies, so Sprint 2 codes against a fixed target.

#### 2. Agent-side local persistence — **recommendation: split file model, not an embedded DB**

The agent has a tiny, low-write, low-concurrency state surface (one device, a handful of records). An embedded SQL engine (sqlite via `mattn/go-sqlite3` needs cgo; `modernc.org/sqlite` is pure-Go but heavyweight) is over-engineered and complicates the atomic-rename + ACL story. A single embedded KV (`go.etcd.io/bbolt`, pure-Go, single-file, ACID, no cgo) is the right tool *if* we wanted one store — but mixing operator-editable config with machine secrets in one binary blob breaks ARCH-8's "operator can edit config, cannot touch secrets" separation.

**Decision: three distinct on-disk artifacts under `C:\ProgramData\BeyzBackup`,** each with its own owner, format, and protection class:

| Artifact | Path | Format | Writer | Protection |
|---|---|---|---|---|
| **Config** (operator-editable, non-secret) | `config.yaml` | YAML | operator / installer | ACL: SYSTEM+Admins RW, Users R (no secrets in it) |
| **State store** (machine identity, mutable, secret) | `state\agent-state.db` | **bbolt** single file | agent service (SYSTEM) | DPAPI-wrapped values **inside** bbolt; ACL: SYSTEM+Admins only, **Users removed** |
| **Device GUID** (immutable identity anchor) | `state\device.guid` | 36-char UUIDv4, plaintext | agent, write-once | ACL as above; survives app reinstall (ARCH-7) |

Rationale for **bbolt** over loose JSON files for the state store: ACID single-file writes give crash safety for free, and a single handle is easier to ACL-lock than N sidecar files. It stays pure-Go (no cgo → trivial cross-compile for the Linux/systemd prep). The device GUID is kept as a *separate* tiny file (not inside bbolt) precisely so it survives a corrupt/rebuilt state DB and an app-only reinstall — it is the stable anchor for license-seat reuse (ARCH-7, LIC-2).

#### 3. State store contents (bbolt buckets) — the frozen agent record set

```
state-version: 1                         # schema_version of this store (SEC-8)
bucket "identity":
  device_guid            -> UUID         # mirror of device.guid (self-generated, ARCH-7/SEC-5)
  device_id              -> string       # SERVER-issued opaque id, bound at enrollment (LIC-2: NOT a hw hash)
  tenant_id              -> string        # immutable, from enrollment (ARCH-6)
  parent_account_id      -> string|null  # MSP hierarchy (LIC-6)
bucket "credentials"  (every value DPAPI-machine-scope wrapped):
  agent_private_key      -> PKCS#8 (CNG/DPAPI)   # generated locally, never leaves device (SEC-5)
  agent_certificate      -> X.509 PEM            # CSR-signed by SaaS at enrollment (SEC-5)
  saas_spki_pin          -> base64 SPKI hash[]   # pinned server cert set (SEC-4)
  license_blob           -> signed bytes         # server-signed, verified each load (LIC-4/LIC-5)
  wrapped_device_key     -> bytes|null           # RESERVED, populated Sprint 4 (BKP-8/SEC-1)
  key_wrap_version       -> int|null             # RESERVED (RST-1)
bucket "runtime":
  last_task_cursor       -> opaque string        # task-poll resume token (ARCH-1 GET /tasks)
  current_agent_version  -> semver               # anti-rollback floor (UPD-6)
  next_poll_delay_sec    -> int                  # server-directed cadence (SCALE-1)
  protocol_version       -> int                  # negotiated control-plane version (SEC-8)
# NOTE (§0.1/§0.6): the updater FSM state is NOT a bbolt bucket. It lives in the
# separate, updater-owned state\updater_state.json. The on-demand updater is a
# DIFFERENT process and cannot share the agent's single-writer bbolt handle.
# (update_state / staged_version / backup_binary_hash live in updater_state.json.)
bucket "audit-spool":
  seq                    -> monotonic uint64      # hash-chain sequence (GAP-7/SEC-7)
  <seq> -> audit_event_json (with prev_hash)      # offline spool until SaaS ships them (ARCH-9)
```

**Must be encrypted at rest** (DPAPI machine-scope value-level wrap *inside* bbolt, per SEC-6/STO-1): `agent_private_key`, `license_blob` (tamper-protection — it's verified by signature, but stored wrapped), `wrapped_device_key`, and any future `storage_credentials`. The certificate, GUID, IDs, cursors, and version numbers are non-secret and stored plaintext within the (ACL-locked) store. Storage credentials are a **distinct secret class** (STO-1) and are *not* persisted in Sprint 1 — a `credentials` slot is reserved, but the design intent is just-in-time delivery over the authenticated channel.

#### 4. Atomicity & crash safety (ARCH-8)

- bbolt already provides ACID transactions — one `Update()` per state mutation; no torn writes.
- The `device.guid` file and any future plaintext sidecar use **write-temp → fsync → rename** (`os.Rename` is atomic on NTFS same-volume).
- The **updater FSM state is persisted in `state\updater_state.json` (atomic write-temp-rename) before each side-effecting step** so a power loss mid-update is recoverable (the **on-demand updater** reads it on its next invocation and resumes/rolls back). It is a **separate, updater-owned file — NOT a bbolt bucket** (the agent is the sole bbolt writer; a different process cannot share bbolt's single-writer lock — §0.1). State machine: `IDLE → MANIFEST_VERIFIED → BINARY_STAGED → BINARY_VERIFIED → SERVICE_STOPPED → SWAPPED → HEALTH_PENDING → COMMITTED` with a `ROLLBACK` branch reachable from any post-`SWAPPED` state (UPD-3/UPD-4/SEC-9).

#### 5. ACL & permission requirements (installer-enforced — PRD req 15, SEC-6)

The Inno Setup installer sets these at folder-create time (`icacls`):
```
C:\ProgramData\BeyzBackup           SYSTEM:(OI)(CI)F  Administrators:(OI)(CI)F   [Users removed]
C:\ProgramData\BeyzBackup\state     SYSTEM:F  Administrators:F                   [inherit broken, no Users]
C:\ProgramData\BeyzBackup\logs      SYSTEM:(OI)(CI)F  Administrators:(OI)(CI)F  Users:(OI)(CI)R
C:\ProgramData\BeyzBackup\config.yaml  SYSTEM:F Administrators:F  Users:R
```
`state\` is the most-restricted: **no Users read** (holds wrapped key + cert). Logs are Users-readable (support diagnostics) but never contain secrets (see Logging). On Linux/systemd prep the equivalent is `/var/lib/beyz-backup` root-owned `0700`, state files `0600`.

#### 6. Minimal server-side tables the contract implies (Sprint 2 forward-declaration)

These are **not created in Sprint 1** — they document what the four OpenAPI endpoints must persist, so Sprint 2 is unblocked. Every table leads with `tenant_id` as the first column of its composite PK/index, per SCALE-4 (shared-schema, mandatory tenant guard / row-level security). Presence is **Redis, not Postgres** (SCALE-2): heartbeats update a Redis key/sorted-set with TTL; only online↔offline *transitions* flush to the `devices` table.

```sql
-- enrollment_tokens  (POST /v1/enroll consumes one)
(tenant_id, token_id PK) token_hash, parent_account_id, expires_at,
  state ENUM('issued','consumed','revoked'),   -- atomic server-side single-use (SEC-2)
  consumed_by_device_id, consumed_at, created_by_actor

-- devices  (POST /v1/agents/{id}/register creates; identity record)
(tenant_id, device_id PK) device_guid, parent_account_id,
  cert_spki_thumbprint, cert_pem, cert_not_after,           -- SEC-5
  agent_version, supported_format_versions JSONB,           -- BKP-7
  hw_signals JSONB,                                         -- clone-detection only, advisory (LIC-2)
  status ENUM('online','offline','revoked'), last_state_change_at,  -- transitions only (SCALE-2)
  license_seat_id, created_at

-- heartbeats  (FLUSH target only — hot path is Redis, SCALE-2)
(tenant_id, device_id, observed_at) agent_version, status_enum,
  reported_usage_bytes,                                     -- LIC-3 reserved metering
  last_error, health JSONB                                 -- GAP-9 fleet health

-- tasks  (GET /v1/agents/{id}/tasks serves these; Redis queue is source of truth)
(tenant_id, task_id PK) device_id, type, payload JSONB,
  schema_version INT, cursor_token,                         -- ARCH-1 poll cursor
  state ENUM('queued','dispatched','acked','done','failed'), created_at

-- audit_events  (ARCH-9 / GAP-7 — append-only, hash-chained, WORM target)
(tenant_id, event_id PK) device_id, event_type, actor, outcome,
  ts_server, prev_hash, this_hash, seq                      -- hash chain anchored server-side
```

**Ownership:** `devices`/`enrollment_tokens`/`audit_events` owned by the *Identity & Enrollment* service (the Enrollment CA boundary, ARCH-3/ARCH-6); `tasks` by the *Job Queue* (Redis primary, Postgres mirror); `heartbeats` presence by *Redis*. Isolation guarantee to document in ARCHITECTURE.md: **shared schema + mandatory `tenant_id` leading column + Postgres RLS**, with declarative partitioning by `tenant_id` reserved for `heartbeats`/`tasks` hot tables (SCALE-4).

---

### Configuration Design

#### 1. `config.yaml` schema (operator-editable, **non-secret only**)

Located at `C:\ProgramData\BeyzBackup\config.yaml`. Carries a top-level `config_version` integer (forward-compatible envelope, ignore-unknown-fields parsing per SCALE-9). Library: **`spf13/viper`** for layered load + env binding, **`go-playground/validator`** for startup validation.

```yaml
config_version: 1

server:
  url: "https://api.beyzbackup.com"      # control-plane base; /v1/ paths appended (ARCH-2)
  spki_pins:                              # SEC-4 cert/pubkey pinning, list for rotation
    - "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
  tls_min_version: "1.2"                  # 1.2 floor, prefer 1.3 (SEC-4)
  protocol_version: 1                     # control-plane version sent as header (SEC-8)

tenant:
  tenant_id: ""                           # populated post-enrollment (immutable thereafter, ARCH-6)
  parent_account_id: ""                   # MSP hierarchy (LIC-6)

enrollment:
  endpoint: "/v1/enroll"
  # token is NEVER stored here — supplied at enroll time via installer dialog / stdin / secure file
  token_source: "prompt"                  # prompt | file | stdin  (NOT plaintext cli by default, SEC-2)

intervals:
  heartbeat_seconds: 60                   # DEFAULT ONLY — server-returned value overrides (SCALE-1)
  task_poll_seconds: 300                  # default; server may override per-response
  jitter_percent: 20                      # mandatory ± jitter (SCALE-1)
  backoff_max_seconds: 900                # exp backoff + full jitter ceiling on errors

network:
  http_keepalive: true                    # mandatory connection reuse (SCALE-3)
  proxy: ""                               # explicit proxy or "" = system proxy (GAP-8)
  proxy_user: ""                          # username only; password via secret channel, not here

logging:
  level: "info"                           # trace|debug|info|warn|error
  format: "json"                          # structured JSON (only format; "console" for dev)
  dir: "C:\\ProgramData\\BeyzBackup\\logs"
  max_size_mb: 50
  max_age_days: 30
  max_backups: 10
  windows_event_log: true                 # mirror security events to Event Log
  security_stream: "security.log"         # separate audit stream filename

updater:
  channel: "stable"                       # stable | beta  (server-directed rollout still gates, UPD-5)
  manifest_endpoint: "/v1/agents/{id}/tasks"  # update offers ride the polling channel (UPD-5)
  allow_downgrade: false                  # anti-rollback; only signed emergency flag overrides (UPD-6)

storage:                                  # RESERVED placeholders — no target wired in Sprint 1 (STO-1/STO-2)
  targets: []                             # future: type(smb|sftp|s3|minio|beyz), capability flags
  # credentials are NEVER in this file — JIT delivery / DPAPI store only (STO-1)

backup:                                   # RESERVED — forward-compatible policy shape (GAP-6)
  schedule_window: ""                     # reserved; enforcement Sprint 3
  bandwidth_limit_kbps: 0                 # 0 = unlimited; reserved
```

#### 2. Precedence — **defaults < file < environment < CLI flag** (CLI wins)

Standard Viper layering. CLI flags (`spf13/pflag`) bind over env, which binds over the YAML file, which binds over compiled defaults. Rationale: an operator debugging on a box must be able to override one value (e.g. `--log-level=debug`) without editing the ACL-locked file; automation sets env; the file is the durable baseline. **Env prefix:** `BEYZ_` with nested keys via `_` (e.g. `BEYZ_LOGGING_LEVEL`, `BEYZ_SERVER_URL`).

#### 3. Secret handling — **secrets are NEVER in `config.yaml`** (CLAUDE.md §27–31, reinterpreted ARCH-8)

| Secret | Where it lives | Why not config.yaml |
|---|---|---|
| Enrollment token | transient: installer dialog → stdin/secure-file → memory, **zeroed after use** (SEC-2) | single-use, short TTL; must not persist or hit installer logs |
| Agent private key / cert | bbolt `state\`, DPAPI-wrapped (SEC-5) | machine credential, OS-protected |
| License blob | bbolt `state\`, signature-verified each load (LIC-5) | tamper-evident, server-signed |
| Device data key (Sprint 4) | bbolt `wrapped_device_key`, never plaintext, never sent (BKP-8) | zero-knowledge boundary |
| Storage credentials (later) | JIT over authenticated channel; if persisted, DPAPI (STO-1) | distinct secret class |

The **reconciliation of CLAUDE.md's "always use env vars for sensitive config"** (ARCH-8): the rule is read as **"no secrets in source code or plaintext config files."** Env vars are acceptable for *injecting* a transient secret (e.g. CI), but the durable resting place for machine secrets is the DPAPI-wrapped, ACL-locked state store — env vars are world-readable to the process tree and survive in crash dumps, so they are **not** the resting place for the private key. The config schema *physically separates* a `*_source` indirection field (e.g. `token_source`, `proxy_user`) from secret material so the loader never logs a secret value.

#### 4. Startup validation (fail-closed)

On service start, before any network call, validate via `go-playground/validator`:
- `config_version` is known/supported; reject and log `config.version.unsupported` if newer than the binary understands (SEC-8 floor semantics).
- `server.url` is a valid `https://` URL (reject `http://` for the control channel, SEC-4); at least one `spki_pins` entry present and well-formed; `tls_min_version >= 1.2`.
- Intervals within sane bounds (`heartbeat_seconds` 10–3600, `jitter_percent` 0–50, `backoff_max_seconds >=` heartbeat).
- `logging.dir` exists and is writable; ACLs as expected (warn if `Users` has write on `state\`).
- `updater.channel` in enum; `allow_downgrade=false` unless explicitly set.

A validation failure **stops the service start** (event-logged), rather than running with unsafe defaults — security-over-speed (CLAUDE.md §23).

#### 5. Hot-reload — **recommendation: selective, not full**

**Do NOT hot-reload identity/security fields** (`server.url`, `spki_pins`, `tenant_id`, `protocol_version`, `updater.*`) — these define the trust boundary and changing them at runtime is an attack surface; they require a service restart. **DO hot-reload operational fields**: `logging.level`, `intervals.*`, `network.proxy`, `backup.bandwidth_limit_kbps`. Implement with Viper's `WatchConfig`/`fsnotify`; on change, re-validate the *whole* file and apply only the allowlisted operational keys atomically, logging a `config.reloaded` event with the changed key set (never values, in case of future secret leakage). Rationale: operators need to bump log level or tune cadence on a live fleet without a restart, but the trust anchor must never silently move.

#### 6. File location & ACLs

`C:\ProgramData\BeyzBackup\config.yaml` — installer-created with `SYSTEM:F Administrators:F Users:R` (read-only for non-admins so a standard user can inspect settings but not weaken them). The file holds **no secrets**, so Users-read is acceptable. On Linux: `/etc/beyz-backup/config.yaml`, `root:root 0644` (or `0640` if a service group is used).

---

### Logging Design

#### 1. Structured JSON, two streams — **recommendation: `rs/zerolog`**

**Library: `rs/zerolog`** (zero-allocation, native JSON, leveled, fast) over `zap`/`slog` — its allocation profile matters for a headless agent that may log heavily during backup phases later, and its API makes a strict field schema easy to enforce. Wrap it behind a small internal `Logger` interface (DI per CLAUDE.md) so the backend can swap without touching call sites.

**Two physically separate streams** (SEC-7 / GAP-7):
1. **Operational log** — `logs\agent.log` — diagnostics, lifecycle, errors. Rotated, Users-readable.
2. **Security/audit stream** — `logs\security.log` — enrollment, update, auth-failure, integrity, restore events. **Hash-chained**, mirrored to the bbolt `audit-spool`, and shipped to the SaaS as soon as Sprint 2's endpoint exists (ARCH-9). This is the schema-stable, tamper-evident record.

#### 2. Levels

`trace` (wire-level dev only) `< debug < info < warn < error`. Default `info`. Security events are emitted at their own severity but **always** also written to the security stream regardless of operational level (you cannot silence the audit trail by lowering log level).

#### 3. Operational log field schema (every record)

```json
{
  "ts": "2026-06-08T10:15:30.123Z",   // RFC3339 UTC, server-time-corrected if skew known (GAP-5)
  "level": "info",
  "component": "enrollment",          // enrollment|heartbeat|taskpoll|updater|config|service
  "agent_id": "dev_7f3a...",          // server device_id, or device_guid pre-enrollment
  "tenant_id": "t_abc",               // present once enrolled (ARCH-6)
  "event": "heartbeat.sent",          // dotted controlled vocabulary (see below)
  "trace_id": "01J...",               // per-operation correlation id
  "agent_version": "1.0.0",
  "msg": "human readable",
  "...": "event-specific fields"
}
```

#### 4. Security/audit event schema (the **frozen** schema, ARCH-9/GAP-7)

This shape must not change in Sprint 8 — only its *transport* gets added. Each record is hash-chained: `this_hash = BLAKE3(prev_hash || canonical(event))`, `seq` monotonic, mirrored to bbolt `audit-spool`.

```json
{
  "schema_version": 1,
  "seq": 42,
  "prev_hash": "blake3:....",
  "ts_local": "2026-06-08T10:15:30.123Z",
  "ts_server": null,                  // filled when SaaS anchors it (server-authoritative)
  "event_type": "enrollment.succeeded",
  "tenant_id": "t_abc",
  "device_id": "dev_7f3a...",
  "actor": "system",                  // system | installer | admin:<id>
  "outcome": "success",               // success | failure | denied
  "detail": { "...": "non-sensitive context only" }
}
```

#### 5. What **MUST** be logged (SECURITY.md §57–68)

| Domain | Events (`event_type`) |
|---|---|
| Enrollment | `enrollment.requested`, `enrollment.succeeded`, `enrollment.failed`, `enrollment.token_rejected` (single-use/expired) |
| Auth | `auth.failure`, `cert.renewed`, `cert.rejected`, `spki_pin.mismatch` (MITM signal, SEC-4) |
| Update | `update.offered`, `update.manifest_verified`, `update.signature_invalid`, `update.hash_mismatch`, `update.staged`, `update.swapped`, `update.health_ok`, `update.rolled_back`, `update.downgrade_blocked` (UPD-6) |
| Integrity | `integrity.check_failed`, `config.tamper_detected`, `license.signature_invalid` (LIC-5) |
| Restore (later) | `restore.started`, `restore.integrity_result`, `restore.completed`, `restore.quarantined` (RST-7) |
| License | `license.issued`, `license.renewed`, `license.revoked`, `license.over_quota` (LIC-1) |
| Lifecycle | `service.started`, `service.stopped`, `config.reloaded`, `decommission.started` (GAP-4) |

#### 6. What **MUST NEVER** be logged

- **Enrollment tokens, private keys, the license blob bytes, wrapped/unwrapped data keys, storage credentials, proxy passwords** — at any level, in any stream. Logging helpers take a typed `Secret` wrapper whose `String()`/`MarshalJSON()` returns `***REDACTED***`, so a secret cannot be accidentally formatted into a record.
- **Full plaintext file paths and usernames** in the *operational* log are PII-sensitive (GAP-7): paths are **hashed or last-segment-only** by default; full paths only at `debug` and only when an operator opted in. The security stream records *that* an event occurred, not the file contents or full path.
- **Raw HTTP Authorization headers / bearer values** — redacted by the HTTP client's logging middleware.

#### 7. Rotation & retention

`natefinch/lumberjack` as the zerolog writer: `max_size_mb=50`, `max_backups=10`, `max_age_days=30`, compressed. **Exception:** the **security stream is never rotated-to-deletion locally without first confirming SaaS ingestion** — once shipping exists (Sprint 8), local security records are pruned only after server-side acknowledgement; until then they accumulate in the bbolt spool (bounded, alert at a size threshold) so a local wipe cannot erase the trail (SEC-7/GAP-7). Log dir ACLs per SEC-6 (Users:R on operational; security stream readable but its authoritative copy lives server-side and is WORM).

#### 8. Windows Event Log integration

Security-class events (and service start/stop/fatal) are **mirrored to the Windows Event Log** under source `BeyzBackup` via `golang.org/x/sys/windows/svc/eventlog`, so domain admins see enrollment/update/auth-failure/tamper events in their existing SIEM/monitoring without the SaaS. The Event Log is **a mirror, not the primary** — the JSON security stream + server copy remain authoritative. The installer registers the event source at install time. On Linux/systemd, the equivalent is structured journald output (`sd-journal` fields) plus the same JSON file.

---

**Cross-references locked in by this section:** device GUID + server-issued `device_id` (ARCH-7/LIC-2), DPAPI-wrapped state with Users-removed ACLs (SEC-5/SEC-6/STO-1), reserved `wrapped_device_key`/`key_wrap_version` so Sprint 4 crypto needs no on-disk break (SEC-1/BKP-8/RST-1), `config_version`/`schema_version` envelopes everywhere (SEC-8/SCALE-9/BKP-7), server-directed jittered cadence fields (SCALE-1), Redis-presence/Postgres-transition heartbeat split (SCALE-2), and the hash-chained, schema-frozen audit stream emitted from day one (ARCH-9/GAP-7/SEC-7).

---

## §4. Updater & Installer Architecture

> The subsections below are the detailed design. Where they name a library, endpoint count, state-store location, or Go version that conflicts with §0, **§0 wins** (these were independent design tracks).

This section specifies the design for `beyz-backup-updater.exe` and the Inno Setup installer. Sprint 1 ships **placeholder business logic**, but every wire format, on-disk layout, trust anchor, and state machine described here is **real and frozen now** — because they are baked into every installed agent and cannot be retrofitted once a fleet exists (UPD-1, UPD-2, ARCH-4, ARCH-2). The Sprint 1 verification code path is **enforcing, never a no-op stub** that returns `true` (CLAUDE.md "no placeholder security"; ARCH-4; SEC-3).

---

### 1. Updater Architecture

> **§0.6 governs this section.** §0.6 is the single source of truth for updater scope; any wording below that implies a second persistent service, a watchdog service, or an updater self-update bootstrap in Sprint 1 is **void** and has been corrected to match §0.6.

#### 1.1 Why a separate binary (not a second service in Sprint 1)

A running Windows Service holds an exclusive lock on its own `.exe` image; a process cannot overwrite the binary backing it. The updater is therefore a **distinct executable** (`beyz-backup-updater.exe`), not a goroutine inside the agent — so it can replace the agent binary that the agent itself cannot.

**Sprint 1 delivery model (per §0.6 — authoritative):** `beyz-backup-updater.exe` is **installed as a separate binary but is NOT registered as a persistent second Windows Service.** It is **invoked on demand** — by the agent (e.g. on an `update_check` task) or by an SCM scheduled task — runs the update FSM to completion, and exits. The persistent **watchdog** role (an always-on second SYSTEM process that recovers a dead agent) and the **updater self-update / two-stage bootstrap** are **deferred to Sprint 8**; for Sprint 1 the agent service's Windows **Service recovery actions** (restart-on-failure, set by the installer) are the backstop. This honors the PRD's "placeholder logic," keeps Sprint 1 to a single service, and leaves every security control (signature verify, hash check, anti-rollback, ACL lockdown, TLS/SPKI pin) fully enforcing.

#### 1.2 On-disk layout (current / new / backup)

The updater owns a fixed slot layout under the install dir. The agent never writes here; only the updater (SYSTEM) does. ACLs: `SYSTEM` + `Administrators` full, `Users` removed (SEC-6, STO-1).

```
C:\Program Files\BeyzBackup\
├─ beyz-backup-agent.exe            # current (live) agent binary
├─ beyz-backup-updater.exe          # current updater binary
├─ trust\
│  └─ update_keys.json              # READ-ONLY pinned pubkey set (informational; real anchor is compiled-in)
└─ staging\                         # SYSTEM/Admin-only; quarantined downloads
   ├─ download.tmp                  # in-flight payload
   ├─ beyz-backup-agent.exe.new     # verified, staged, awaiting swap
   └─ beyz-backup-agent.exe.bak     # last-known-good, kept until health gate passes

C:\ProgramData\BeyzBackup\state\
└─ updater_state.json               # atomic (write-temp-rename); the update FSM's durable state
```

The bundle versions **binary + config-schema together** (UPD-4). `updater_state.json` records the config snapshot reference so rollback restores a **consistent (binary, config) pair**, never a mismatched one.

#### 1.3 Update manifest format

A single canonical JSON document, fetched over the **pinned SaaS endpoint** (`GET /v1/agents/{id}/update?channel=stable`, SEC-4, UPD-7). The **Ed25519 signature covers the entire canonicalized manifest**, not just the binary hash (UPD-7, UPD-8). Reserved fields exist from day one even when unused until later sprints.

```jsonc
{
  "schema_version": 1,                         // BKP-3/UPD-7 versioned envelope
  "protocol_version": 1,                       // SEC-8 / ARCH-2 negotiation
  "key_id": "beyz-upd-2026-01",                // UPD-2 rotation: which embedded pubkey signed this
  "channel": "stable",                         // stable | beta | canary
  "target_version": "1.4.0",                   // SemVer
  "min_supported_version": "1.2.0",            // UPD-6 anti-rollback floor (signed, not a header)
  "released_at": "2026-06-08T10:00:00Z",
  "rollout": {                                 // UPD-5 server-directed; agent never pulls "latest"
    "cohort_percent": 10,
    "update_allowed": true,                    // kill-switch
    "pin_version": null                        // per-tenant version pin
  },
  "key_revocation_list": ["beyz-upd-2024-old"],// UPD-2 ships from day one, may be empty
  "artifacts": [                               // per-platform array
    {
      "platform": "windows/amd64",
      "component": "agent",                    // agent | updater
      "url": "https://updates.beyzbackup.com/1.4.0/windows-amd64/beyz-backup-agent.exe",
      "size_bytes": 18452992,
      "hash_algo": "blake3",                   // BLAKE3 per CLAUDE.md preference (UPD-8/RST-3)
      "hash": "blake3:9f2c...e1",              // algo-tagged; verifier dispatches on tag
      "sha256": "sha256:ab34...90"             // SECURITY.md mandates SHA256; kept as second check
    }
  ],
  "signature": {
    "algo": "ed25519",
    "key_id": "beyz-upd-2026-01",
    "value": "base64(ed25519-sig-over-canonical-json-minus-signature-block)"
  }
}
```

**Canonicalization:** the signature is computed over the manifest with the `signature.value` field emptied, serialized as **RFC 8785 JSON Canonicalization Scheme (JCS)** so byte-for-byte reproducibility is guaranteed across Go encoders.

#### 1.4 Signature verification & trust anchor (ENFORCING in Sprint 1)

| Decision | Choice | Rationale |
|---|---|---|
| Algorithm | **Ed25519** | Small (32-byte key, 64-byte sig), fast, no padding/parameter pitfalls of RSA-PSS (UPD-1, GAP-3). |
| Trust anchor | **Compile-time embedded public-key *set*** in the updater binary | Cannot be swapped at rest in writable config (UPD-1). A *set* (not single key) makes rotation possible without re-issuing the fleet (UPD-2). |
| Key custody | Private key in **offline air-gapped signing ceremony** or **cloud HSM/KMS** the build pipeline can only *invoke*, never export; **never** on CI runners or laptops; signing is **multi-person + logged** | SEC-3 / UPD-1 — this is the highest-value key in the system; it authorizes SYSTEM-level code execution. |
| Rotation | Manifest carries `key_id`; agent accepts any signature from a non-revoked embedded key; `key_revocation_list` ships in every manifest | Overlapping-key rotation with no fleet re-issue (UPD-2, ARCH-4). |
| Trust as a class | Update-signing key is **separate from and higher-trust than** any TLS/API/license key | UPD-1, LIC-4. |

**Go libraries:** `crypto/ed25519` (stdlib) for verification; `lukechampine.com/blake3` for the BLAKE3 payload hash; `crypto/sha256` (stdlib) for the SHA256 cross-check. No third-party crypto for the signature path.

#### 1.5 Verification-then-execution order (closes TOCTOU)

The hash **must never** come from any source except the signed manifest (UPD-8). Exact order (SEC-9, UPD-8):

1. Fetch manifest over **pinned-cert HTTPS** (SEC-4); reject if `protocol_version` below floor (SEC-8).
2. **Verify Ed25519 signature** over canonical manifest. Reject if `key_id` is in the revocation list or not in the embedded set. **No download trust is granted before this step passes.**
3. Enforce **anti-rollback**: reject if `target_version <= current_version` unless an explicit, separately-signed emergency-downgrade flag is present (UPD-6). Comparison uses the **signed manifest version**, never a filename or server header.
4. Enforce **rollout gate**: proceed only if `rollout.update_allowed == true` and this device is in the cohort (UPD-5). The agent updates **only when the server says so**.
5. Download payload to `staging\download.tmp` (SYSTEM/Admin-only dir).
6. **Re-verify on the exact staged bytes**: BLAKE3 (+ SHA256) of the file equal the manifest values. Verify on the **same open, locked file handle** that will be executed — verify-then-exec on one handle closes the TOCTOU window (SEC-9).
7. Only now promote `download.tmp` → `beyz-backup-agent.exe.new` and enter the swap FSM.

#### 1.6 Atomic swap sequence

```
[Verified .new staged]
  → Stop BeyzBackupAgent service (SCM, bounded timeout; kill if hung)
  → Snapshot config + current binary → staging\*.bak   (consistent pair, UPD-4)
  → MoveFileEx(.new → live path, MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)   // atomic rename, not in-place overwrite (UPD-3)
  → Start BeyzBackupAgent service
  → HEALTH GATE (see 1.7)
      pass → delete *.bak, record new current_version, persist state = idle
      fail → ROLLBACK (see 1.8)
```

`MoveFileEx` with `MOVEFILE_REPLACE_EXISTING` gives an **atomic rename** so an interrupted swap leaves either the old or the new binary intact — never a truncated one (UPD-3).

#### 1.7 Post-update health gate (the rollback trigger)

Rollback is **not** a vague placeholder (UPD-4). The concrete gate after restart:

- New agent must **(a) start**, and **(b) successfully send one heartbeat / `update_status: "ok"` self-report to the SaaS within a bounded timeout (90 s)** before the updater commits.
- The heartbeat ack **is** the update-success signal — the heartbeat sender is already Sprint 1 scope, so wiring it costs nothing now (UPD-4).
- The gate is owned by the **same on-demand updater invocation that performed the swap** (the updater stays resident only for the bounded health-gate window, then exits): the agent writes `state\health.json` (atomic) on a successful post-update heartbeat **and** the updater confirms the agent service is `RUNNING`. Both must be true.

If the gate is not met within the timeout → automatic rollback.

#### 1.8 Rollback mechanism & triggers

**Triggers:** service fails to start; health gate times out (no heartbeat ack in 90 s); agent crash-loops (N restarts in window); updater process itself dies mid-swap (recovered on the **next on-demand updater invocation** by reading the persisted FSM state).

**Mechanism:**
1. Stop the (failed) agent.
2. **Integrity-check the `.bak` copy** (BLAKE3 of backup == recorded value) *before* restoring — never restore a corrupt backup (SEC-9).
3. `MoveFileEx` restore `beyz-backup-agent.exe.bak` → live path **and** restore the paired config snapshot (UPD-4).
4. Start agent; confirm health.
5. Emit `update.rolled_back` audit event (1.10) and report failure to SaaS so the bad version is visible fleet-wide.

#### 1.9 "Who updates the updater?" — DEFERRED to Sprint 8 (§0.6)

A process cannot overwrite its own running image, so updating the **updater itself** needs a two-stage bootstrap (a helper that swaps the updater binary while it is stopped, or a `MOVEFILE_DELAY_UNTIL_REBOOT` fallback). **This is out of Sprint 1 scope (§0.6).**

In **Sprint 1** the updater replaces only the **agent** binary — the simple case where the updater is *not* the binary being replaced. Updating the updater binary itself is handled by the **installer** (a major/manual re-install) until the two-stage bootstrap ships in **Sprint 8**. The on-disk layout reserves room for it, but **no updater self-update code is written in Sprint 1.**

#### 1.10 Updater state machine + persisted state

State is persisted to `state\updater_state.json` via **write-temp-rename (atomic)** so an interrupted updater resumes correctly after crash or power loss (ARCH-8 crash-safety).

```
IDLE ─(manifest: update_allowed & in-cohort & version>current)→ MANIFEST_VERIFIED
MANIFEST_VERIFIED ─(sig+rollback+rollout ok)→ DOWNLOADING
DOWNLOADING ─(hash ok on locked handle)→ STAGED
STAGED → STOPPING_AGENT → BACKED_UP → SWAPPING → STARTING_AGENT → HEALTH_CHECK
HEALTH_CHECK ─ pass →[delete .bak]→ IDLE
HEALTH_CHECK ─ fail → ROLLING_BACK → STARTING_AGENT(old) → IDLE(failure reported)
any state ─(updater crash)→ on restart: read persisted state, resume or roll back
```

```jsonc
// updater_state.json
{
  "fsm_state": "HEALTH_CHECK",
  "current_version": "1.3.0",
  "target_version": "1.4.0",
  "manifest_key_id": "beyz-upd-2026-01",
  "config_snapshot_id": "cfg-1.3.0",   // pairs with binary for consistent rollback
  "attempt": 1,
  "health_deadline": "2026-06-08T10:02:30Z",
  "last_known_good_hash": "blake3:..."
}
```

#### 1.11 Failure modes that must never brick the device

| Failure | Guarantee |
|---|---|
| Power loss mid-swap | Atomic `MoveFileEx` rename → either old or new binary intact; FSM resumes from persisted state. |
| Corrupt download | Caught at step 6 (hash mismatch) before any swap; staged file discarded. |
| Signature forgery / spoofed channel | Rejected at step 2; cert pinning (SEC-4) prevents manifest spoofing. |
| Downgrade attack with valid old signature | Rejected by anti-rollback floor (step 3, UPD-6). |
| New agent crash-loops | Health gate fails → auto-rollback to integrity-checked `.bak`. |
| Corrupt `.bak` | Integrity-checked before restore; if bad, updater re-fetches last-known-good from SaaS rather than restoring garbage (UPD-9). |
| Updater process dies between runs | Expected — the updater is **on-demand, not resident**; its FSM state is durable in `updater_state.json`, so the **next invocation** resumes or rolls back. |
| Agent down + no updater running | **Windows Service recovery actions** on the *agent* service (restart-on-failure, set by the installer) restart the agent; the next scheduled/agent-triggered updater run reconciles against last-known-good. The persistent **watchdog** that recovers a dead agent autonomously is **deferred to Sprint 8** (§0.6, UPD-9). |

#### 1.12 Sprint 1 scope boundary

Sprint 1 ships (all **real and enforcing**): manifest struct + JCS canonicalization, **Ed25519 verification against an embedded test public key** (never `return true`), BLAKE3 + SHA256 hashing, anti-rollback, the FSM with persisted state, and a real `MoveFileEx`/SCM swap **of the agent binary** with integrity-checked rollback — all invoked **on demand** (no second persistent service). **Deferred to Sprint 8 (§0.6):** the persistent watchdog service and the updater self-update (two-stage bootstrap). **Placeholder** = the SaaS update endpoint returns a static test manifest and rollout orchestration/canary cohorts are stubbed server-side. The verification path is **exercised from day one** (ARCH-4).

---

### 2. Installer Architecture (Inno Setup)

#### 2.1 Install layout

| Path | Contents | Writer | ACL |
|---|---|---|---|
| `C:\Program Files\BeyzBackup\` | `beyz-backup-agent.exe`, `beyz-backup-updater.exe`, `staging\`, `trust\` | updater (SYSTEM) | SYSTEM+Admins full; Users read-only; **Users cannot write** (prevents binary-planting). |
| `C:\ProgramData\BeyzBackup\config.yaml` | non-secret operator settings | operator/admin | SYSTEM+Admins RW; **Users removed** (SEC-6). |
| `C:\ProgramData\BeyzBackup\state\` | cert, private key, device GUID, license blob, updater FSM state | agent/updater (SYSTEM) | SYSTEM+Admins only; **DPAPI machine-scope encrypted at rest** (SEC-6, STO-1, ARCH-8). |
| `C:\ProgramData\BeyzBackup\logs\` | structured logs + audit events | agent (SYSTEM) | SYSTEM+Admins only (SEC-7 tamper resistance). |

**config vs state split (ARCH-8, SEC-6):** `config.yaml` holds **only non-secret, operator-editable** settings (endpoints, poll interval defaults, proxy, bandwidth window). All secret material — agent cert, **private key**, device GUID, signed license blob, `wrapped_device_key` — lives in the separate machine-protected `state\` store, **DPAPI machine-scope** encrypted, never in YAML, never env vars. This reconciles CLAUDE.md's "always use environment variables for secrets" to its correct meaning: **"no secrets in source or plaintext config"** (ARCH-8).

#### 2.2 Folder creation with correct ACLs (PRD req 15)

The installer sets restrictive ACLs **at create-folder time**, not after (SEC-6). Inno Setup `[Dirs]` with explicit permissions, or a `CurStepChanged(ssPostInstall)` routine invoking `icacls`:

```
icacls "C:\ProgramData\BeyzBackup"        /inheritance:r
icacls "C:\ProgramData\BeyzBackup"        /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F"
icacls "C:\ProgramData\BeyzBackup\state"  /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F"
:: Users group explicitly NOT granted — config is not world-readable
```

`/inheritance:r` removes inherited `Users` ACEs so config and state are **never world-readable**.

#### 2.3 Windows Service registration

**One service only in Sprint 1 (per §0.6).** The installer registers **`BeyzBackupAgent`**. `beyz-backup-updater.exe` is installed as a binary but is **NOT registered as a persistent service**; it is invoked on demand (by the agent or an SCM scheduled task — see §1.1). The installer MAY create a **disabled/optional scheduled task** for periodic update checks, but **no always-on second service runs**, and the `BeyzBackupUpdater` service registration is **reserved for Sprint 8**, not created now.

| Setting | `BeyzBackupAgent` (the only Sprint-1 service) |
|---|---|
| Account | **`LocalSystem`** (Sprint 1) |
| Start type | `auto` (`SERVICE_AUTO_START`) |
| Recovery | Restart after 1st/2nd failure (5s/30s); reset count daily — **this is the Sprint-1 backstop in place of a watchdog** |
| Dependency | none |

**Account recommendation:** **`LocalSystem` for Sprint 1**, with a documented intent to migrate to a **dedicated low-privilege virtual service account** (`NT SERVICE\BeyzBackupAgent`) once backup file-access requirements are known (Sprint 3+). The **on-demand updater also runs as SYSTEM when invoked** (it stops/starts the agent service and writes `Program Files`), but holds **no persistent service registration**. The agent needs broad read for backups later; `LocalSystem` is the pragmatic Sprint-1 choice and the DPAPI-machine-scope secret store keeps secrets readable across the eventual account change. Recovery/restart actions are set via `[Run]` calling `sc.exe failure` (Inno Setup's native service support does not cover recovery actions).

#### 2.4 Service start post-install (PRD req 17)

After files are placed, ACLs set, and first-run config written, the installer starts **`BeyzBackupAgent`** — the only service. The updater is **not** started as a service; it runs on demand (§1.1). If an enrollment token was supplied (2.5), the agent performs first-run enrollment on startup; otherwise it idles in an **unenrolled** state and retries from config on every start (no crash if unenrolled).

#### 2.5 Enrollment token: wizard page AND silent param

Both paths required (PRD req 18; SEC-2).

- **Interactive:** a custom Inno Setup wizard page (`CreateInputQueryPage`) prompts for the enrollment token (masked input).
- **Silent (MSP mass-deploy):** `/TOKEN=<token>` command-line parameter, read via `{param:TOKEN}`.

**How the token reaches first-run (securely):** the installer **does not** write the token into `config.yaml` (it would persist a single-use secret in plaintext, SEC-2). Instead it writes the token to a **transient, ACL-locked one-shot file** `C:\ProgramData\BeyzBackup\state\enroll.token` (SYSTEM+Admins only). On first start the agent (a) reads it, (b) performs `POST /v1/enroll`, (c) on success **securely deletes** the file. The token is **excluded from installer logs** (`/LOG` redaction) and zeroed from process memory after use (SEC-2). This realizes single-use + short-TTL semantics: server-side atomic consumption is authoritative (SEC-2), and the local artifact self-destructs.

> Token requirements enforced server-side, designed now (SEC-2, ARCH-6, LIC-6): ≥128-bit CSPRNG, single-use via atomic DB-row consumption, short TTL (15–60 min), **tenant-scoped** (binds `tenant_id` + MSP `parent_account_id`). Every enrollment attempt (success/fail) is logged (SEC-2) and emitted through the audit schema (ARCH-9).

#### 2.6 Signed installer + signed binaries (Authenticode)

- **Both `.exe` files Authenticode-signed** *before* packaging, and the **installer `.exe` itself Authenticode-signed** (Inno Setup `SignTool` directive). Rationale: SmartScreen/UAC reputation, and a defense-in-depth identity check independent of the Ed25519 update channel.
- **Authenticode is for distribution identity; the Ed25519 manifest signature is the real update trust root** (UPD-1) — the updater does **not** rely on Authenticode for update decisions, because Authenticode trust is OS-managed and swappable, whereas the Ed25519 key is pinned in-binary.

#### 2.7 Upgrade / repair / uninstall

| Operation | Behavior |
|---|---|
| **Upgrade** (installer-driven) | Stop the `BeyzBackupAgent` service → replace **both binaries** (agent + updater) → **preserve** `config.yaml` and `state\` (cert, GUID, license) → restart the service. Never overwrites enrolled identity. Day-to-day **agent** updates go through the on-demand updater; the installer upgrade path is for major/manual re-installs **and is how the updater binary itself is updated in Sprint 1** (two-stage self-update deferred — §0.6). |
| **Repair** | Re-apply ACLs, re-register the `BeyzBackupAgent` service, replace missing binaries; **never** touch `state\`. |
| **Uninstall** | **Must stop the `BeyzBackupAgent` service first** and remove any updater scheduled task (PRD; GAP-4). Then best-effort `POST /v1/agents/{id}/deenroll` to **release the license seat** (GAP-4, LIC-7). |

**Uninstall — wipe state/keys? Decision:** **default = crypto-shred** the `state\` store (cert, private key, device GUID, `wrapped_device_key`), satisfying GDPR crypto-shred (destroy wrapping key ⇒ data unrecoverable, GAP-4). Provide `/KEEPSTATE` for re-image/device-replacement scenarios that reuse the license seat (ARCH-7). Rationale: secure-by-default offboarding; opt-out for legitimate migration. The installer writes secrets in a location/format the uninstaller can **fully purge** (single `state\` dir).

#### 2.8 Silent install for MSP mass-deployment

```
beyz-backup-setup.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART ^
  /TOKEN=<enrollment-token> /LOG="C:\Windows\Temp\beyz_install.log"
```

`/VERYSILENT` (no UI), `/SUPPRESSMSGBOXES`, `/NORESTART` for RMM/GPO push. Token via `/TOKEN=` (2.5). The install log **excludes the token value** (SEC-2). Per-tenant MSP deployments use distinct tenant-scoped tokens so each device binds to the correct `tenant_id`/`parent_account_id` (ARCH-6, LIC-6).

#### 2.9 Linux systemd prep (note only)

Sprint 1 ships Windows. For forward-compat, document the planned layout so config/state formats are cross-platform identical: binaries `/opt/beyzbackup/`; config/state `/etc/beyzbackup/` (config `0644`) and `/var/lib/beyzbackup/state/` (**root-only `0600`**, future TPM/keyring in place of DPAPI); logs `/var/log/beyzbackup/`. **Per §0.6, Sprint-1 prep ships a single long-running unit `beyz-backup-agent.service` (`Type=notify`, `Restart=on-failure`);** the updater is invoked via a **systemd timer + oneshot unit** (mirroring the Windows on-demand model), **not** a second always-on service. No code in Sprint 1 — note only (PRD req 4).

---

### 3. Cross-cutting Sprint-1 decisions locked by this section

- **Versioning everywhere (ARCH-2, SEC-8, SCALE-9):** `/v1/` in every update path; manifest carries `schema_version` + `protocol_version` + `min_supported_version`; the updater rejects below-floor versions.
- **Trust roots frozen (ARCH-4, UPD-1/2, SEC-3):** Ed25519 pinned key-set compiled into the updater; offline/HSM custody; `key_id` + revocation list shipping from the first installer.
- **Server-directed updates (UPD-5):** agent never pulls "latest"; rollout/kill-switch/version-pin fields exist in the manifest now so canary/staged rollout (Sprint 8) needs **no wire-format break**.
- **Audit events from day one (ARCH-9, SEC-7, GAP-7):** updater emits `update.started`, `update.succeeded`, `update.rolled_back`, `update.signature_failed`, and installer emits `enroll.attempted` through the frozen audit schema (`event_type, tenant_id, device_id, timestamp, actor, outcome`), even though the central audit store arrives in Sprint 8.
- **Crash-safe state (ARCH-8):** all updater/agent state writes are write-temp-rename atomic; secrets in DPAPI-machine-scope `state\`, never config or env.

Key source-of-truth artifacts to commit in Sprint 1: the **update manifest JSON Schema** (frozen, versioned), the **embedded Ed25519 public-key set + `key_id` registry**, and the **updater FSM spec** above — committed to the repo as authority, not derived from the Go code (ARCH-1 principle applied to the update channel).

---

## §5. Security Controls & Test Strategy

> The subsections below are the detailed design. Where they name a library, endpoint count, state-store location, or Go version that conflicts with §0, **§0 wins** (these were independent design tracks).

This section specifies the security controls and test strategy for **Sprint 1 (Agent Foundation)** only. Sprint 1 ships `beyz-backup-agent.exe`, `beyz-backup-updater.exe`, `config.yaml` at `C:\ProgramData\BeyzBackup`, structured logging, enrollment, heartbeat, the task-poll placeholder, the updater skeleton, and the Inno Setup installer. There is **no backup/restore/crypto engine** yet — but per `CLAUDE.md` ("no placeholder security implementations") every security path Sprint 1 *touches* must be real and enforcing from day one, even where the SaaS counterpart is stubbed.

A guiding rule resolves the apparent contradiction between `CLAUDE.md`'s "always use environment variables for sensitive configuration" and a Windows Service that must read its own credentials at boot with no operator present: **`CLAUDE.md`'s rule means "no secrets in source or in plaintext config/repo" — it does NOT mean "all secrets in env vars."** A `LocalSystem` service has no usable per-user env. The authoritative Sprint 1 secret store is the OS keystore (Windows DPAPI machine scope), not environment variables. This reconciliation (ARCH-8, SEC-6) is stated normatively below and must be written back into `SECURITY.md` and `CLAUDE.md`.

---

### Security Controls (Sprint 1)

#### On-disk layout & file ACLs (SEC-6, ARCH-8, STO-1)

The data root `C:\ProgramData\BeyzBackup` separates **operator-editable plaintext** from **machine-protected secrets**. The installer (Inno Setup, PRD req 15) sets restrictive ACLs at folder-create time.

```
C:\ProgramData\BeyzBackup\
  config.yaml            # operator-editable, NON-secret only. ACL: SYSTEM+Admins RW, Users R
  state\                 # machine-protected secret store. ACL: SYSTEM+Admins ONLY (Users removed)
    device.json          # device_id (server-issued), tenant binding (non-secret, integrity-checked)
    device.key.dpapi     # agent private key, DPAPI machine-scope wrapped
    agent.crt            # agent certificate returned by SaaS
    enroll.consumed      # marker proving token was consumed (idempotency)
    license.blob         # server-signed license (LIC-4/5); verified, never trusted as plaintext
    update\              # staging dir for updater; SYSTEM+Admins ONLY (UPD-3, SEC-9)
  logs\                  # ACL: SYSTEM+Admins RW, Users R (no write). Hash-chained (SEC-7)
```

ACL decision (recommended): on the `state\` and `update\` trees, **break inheritance and remove the `Users`/`Authenticated Users` group entirely**, granting only `NT AUTHORITY\SYSTEM` and `BUILTIN\Administrators`. Enforce with `icacls` from `[Run]`/`Pascal` in the Inno Setup script. Rationale: prevents a standard user from reading the wrapped key blob or planting a malicious staged updater binary.

- **`config.yaml` holds NO secrets.** Plaintext keys only: `server.base_url`, poll/heartbeat overrides, `proxy.*`, `log.level`, future `bandwidth.*`/`schedule.*` placeholders (GAP-6, GAP-8). A schema split (`# SECRET — managed by agent, do not edit` banner over the `state/` reference) keeps secrets structurally out of the file so they can never be logged (SEC-6).
- **Secret material lives in `state\` under DPAPI machine scope** via `golang.org/x/sys/windows` (`CryptProtectData` with `CRYPTPROTECT_LOCAL_MACHINE`), not env vars, not plaintext YAML. On Linux (systemd prep only): root-owned `0600`, with TPM/keyring deferred.
- **Atomic, crash-safe writes** (ARCH-8): all `state\` writes use write-temp-`fsync`-rename (`os.Rename` is atomic on NTFS for same-volume). Never partial-write a key or license.

#### Enrollment token handling (SEC-2, ARCH-7, GAP-2)

- **Format:** ≥128-bit CSPRNG opaque value, base32/base58 with a short prefix + checksum (e.g. `bzt_<base32>`) for typo detection. Tenant-scoped server-side (ARCH-6, LIC-6).
- **Transport:** **TLS only**, over the pinned control channel (see TLS below). The token rides in the `POST /v1/enroll` request body, never a URL query param (avoids proxy/access-log leakage).
- **Single-use & TTL:** single-use is enforced **server-side by atomic row-state consumption** (DB transition `issued → consumed`), never by client trust (SEC-2). Recommended TTL **30 minutes** (PRD/installer flow is interactive; 15–60 min window, 30 is the default). Server returns `409 token_already_consumed` / `410 token_expired`.
- **Input path:** installer accepts the token via **silent-install parameter** (`/ENROLLTOKEN=...`) **or** an interactive dialog (PRD req 18). The token is passed to the agent's one-shot `enroll` subcommand via **stdin or a 0600 temp file that is deleted on consume — not as a persistent CLI argument** (CLI args are visible in process listings and can land in installer logs). If a CLI arg path is supported for automation, the agent must **zero it from memory after read and the installer must exclude it from `/LOG`** (SEC-2).
- **Never logged:** the token value is on the redaction denylist (see Logging). Log only `enroll.attempt`, the token's **last 4 chars + a SHA256 prefix** for correlation, and outcome — never the full token.

#### Agent fingerprint / device identity (LIC-2, SEC-5, ARCH-7)

**Decision: the primary device identity is a server-issued opaque `device_id`, NOT a client-computed hardware hash.** Hardware signals are advisory clone-detection heuristics only.

- At first run the agent generates a **persisted random device GUID** in `state\device.json` (survives app reinstall, since `ProgramData` is preserved by the uninstaller's data-keep option). It generates a **keypair locally** and submits a **CSR** in the enrollment request (SEC-5).
- The SaaS issues an opaque `device_id` and signs/returns the **agent certificate**, binding `device_id` + immutable `tenant_id` (+ `parent_org_id` for MSPs, ARCH-6/LIC-6) into the cert. **The cert's SPKI thumbprint + the device GUID is the fingerprint** (SEC-5) — not raw hardware serials.
- **Advisory hardware signals** (Windows `MachineGuid` from registry + system disk serial + first NIC MAC) are reported at enrollment **for clone-detection and re-enrollment matching only** (LIC-2). They are NOT the license key. VM-clone handling: a cloned `config.yaml`/state without a matching server-side cert+device record is detectable server-side; document the **re-image/clone re-enrollment flow that reuses the license seat** (ARCH-7).
- **Stability vs uniqueness vs privacy:** the device GUID gives stability across hardware changes; the cert gives uniqueness and tamper-evidence; hardware signals are hashed/salted where feasible and treated as low-sensitivity (privacy: no usernames or file paths in the fingerprint).

#### Credential / certificate storage (SEC-5, SEC-6, STO-1)

- Agent **private key** → DPAPI machine-scope wrapped (`device.key.dpapi`); never leaves `state\`, never transmitted, never logged.
- Agent **certificate** → `agent.crt` in `state\`; treated as a **client credential**, laying groundwork to promote to true mTLS in Sprint 8 **without changing the enrollment format** (SEC-4).
- **Customer storage credentials are a distinct secret class** (STO-1): a `CredentialStore` abstraction and a config *slot* are defined now, but no target is wired in Sprint 1; when they arrive they will be delivered just-in-time over the authenticated channel or DPAPI-persisted — never plaintext in `config.yaml`.
- **License blob** is stored but **its signature is verified against an embedded public key on every load** (LIC-5); a plaintext license is never trusted.

#### TLS configuration & pinning (SEC-4, GAP-5, GAP-8)

- **TLS 1.2 minimum, 1.3 preferred** (`tls.Config{MinVersion: tls.VersionTLS12}`), enforced on the agent HTTP client (Go `net/http` + custom `tls.Config`).
- **Server certificate validation is mandatory** AND the control channel **pins the SaaS server's SPKI** (public-key pin) via a custom `VerifyConnection`/`VerifyPeerCertificate` callback. **Reject system-CA-only trust for the control channel** (SEC-4) — this is the Sprint 1 substitute for mTLS and is what makes plain TLS+token acceptable until Sprint 8. Pin a **small set** of SPKI hashes to allow rotation; **document the pin-rotation procedure now**.
- **HTTP keep-alive / connection reuse is mandatory** on the agent client so the future mTLS handshake amortizes across polls (SCALE-3).
- **Proxy & dual-stack** (GAP-8): system-proxy detection plus explicit `proxy.{host,port,user,pass}` in config; verify IPv6 works in the Go client; document the egress FQDN/port allow-list and the corporate-TLS-interception stance (configured corporate CA may be added to the *system* trust, but the **control channel still requires the SPKI pin**, so an intercepting proxy must be explicitly whitelisted, not silently trusted).

#### Update signature: public-key embedding & verification (UPD-1/2/6/8, SEC-3, ARCH-4, GAP-3)

This is the single highest-value trust decision and **must ship enforcing in the first installed binary**, even though signed updates "ship" in Sprint 8.

- **Algorithm: Ed25519** over a **canonical JSON** update manifest (`golang.org/x/crypto/ed25519`, std `crypto/ed25519`). Rationale: small, fast, no padding pitfalls; matches UPD-1.
- **Trust anchor: a pinned key-SET embedded as a compile-time constant** in the updater binary (UPD-2). The manifest carries a **`key_id`** so keys rotate without re-issuing every agent; the agent accepts a signature from any embedded, **non-revoked** key. The manifest format reserves a (possibly empty) **key-revocation-list field from day one**.
- **Verification order is fixed and documented** (UPD-8): (1) fetch manifest over the **pinned** SaaS endpoint; (2) verify Ed25519 signature over the **whole canonical manifest**; (3) read the expected binary hash **from the now-trusted manifest**; (4) download binary into `state\update\`; (5) verify hash of the **exact downloaded bytes**; (6) only then atomic-replace. **The hash must never come from any source other than the signed manifest.**
- **Hash choice:** for the **update path**, follow `SECURITY.md`/PRD and use **SHA256** (named in the manifest with an explicit `hash_algo` tag so it can become BLAKE3 later). Keep BLAKE3 reserved for the *backup* path (RST-3/BKP-5) — the tag avoids ambiguity.
- **Anti-rollback** (UPD-6, SEC-9): manifest carries a **monotonic version** + **min-acceptable-version floor**; agent persists current version in `state\` and **refuses any update ≤ current** unless a separately-flagged, root-signed emergency-downgrade is present. Comparison uses the **signed manifest version**, never a filename or server header.
- **Sprint 1 implements verification against a real embedded TEST public key — never a `return true` stub** (ARCH-4). The private test key is offline; production custody (HSM/offline ceremony, multi-person, logged — SEC-3/UPD-1) is documented now but not exercised until Sprint 8.
- **Replace mechanic & rollback gate** (UPD-3/4): write-new → verify → `MoveFileEx` atomic rename **of the agent binary** → start service → **post-start health gate** (new agent must send one heartbeat / "update OK" within **90s**) → only then delete `.exe.bak`. On gate failure, restore `.exe.bak` **and** the prior config snapshot and restart. Runs **on demand** (no second persistent service). **Updater self-update (two-stage bootstrap) is deferred to Sprint 8 (§0.6)** — in Sprint 1 the updater replaces only the agent binary; the updater binary itself is updated via the installer.

#### Secret handling & no-secrets-in-source enforcement (CLAUDE.md, SEC-6)

- **Reconciled rule (normative):** no secrets in source, repo, or plaintext config; runtime secrets live in DPAPI-protected `state\`. Env vars are acceptable only for **non-service, developer/CI** configuration, never as the service's secret store.
- **CI gate:** add `gitleaks` (or `trufflehog`) as a required pipeline step that fails the build on any committed secret/key. The **update test private key is stored in the CI secret manager, not the repo**; only the **public** key is compiled in.
- **No `return true` security stubs** anywhere in the shipped binaries (CLAUDE.md "Forbidden").

#### Least privilege for the service account (ARCH-8)

- The **`BeyzBackupAgent`** Windows Service (the only Sprint-1 service) runs as **`LocalSystem`** (required because it must read machine-scope DPAPI state and, later, snapshot/back up arbitrary files). **Recommended hardening:** evaluate `NT SERVICE\BeyzBackupAgent` virtual service account in a later sprint to drop privileges; document the rationale for `LocalSystem` now. The **updater runs at SYSTEM only when invoked** (it stops/starts the agent service and replaces files under `Program Files`) — its staging dir is locked SYSTEM+Admins only (SEC-9). It is **on-demand, not a persistent service** (§0.6).
- Configure **Windows Service recovery** (restart-on-failure) on the **agent** service as the Sprint-1 backstop; authoritative recovery compares against the **last-known-good binary** `.exe.bak`, not a blind restart. **Decision (§0.6): the updater is on-demand in Sprint 1, NOT a persistent watchdog** — the persistent watchdog that detects prolonged agent absence is **deferred to Sprint 8**; SCM recovery actions cover Sprint 1 (UPD-9).

#### Audit / security-event logging & tamper basics (SEC-7, ARCH-9, GAP-7)

- **Structured logging** via `log/slog` (Go std) or `zap`, JSON output to `logs\`. **Security events are first-class records** with a **frozen schema** so Sprint 8 only adds transport, not a format break (ARCH-9):

```json
{ "event_type": "enroll.success", "tenant_id": "...", "device_id": "...",
  "ts": "2026-06-08T12:00:00Z", "actor": "agent", "outcome": "ok",
  "seq": 42, "prev_hash": "blake3:...", "hash": "blake3:..." }
```

- **Sprint 1 emits enrollment and update events through this schema** even though the central audit store is Sprint 8 (ARCH-9). Event types: `enroll.{attempt,success,fail}`, `update.{check,verify_ok,verify_fail,applied,rolled_back}`, `auth.failure`, `license.{verify_fail}`, `config.tamper`.
- **Tamper-evidence basics (Sprint 1):** **per-record hash chaining** — each entry includes `prev_hash` (SEC-7/GAP-7). Plan **near-real-time shipping of security events to the SaaS** so the authoritative copy is off the endpoint (transport stubbed in Sprint 1, schema final now). Log-dir ACLs restrict write (above).
- **Redaction denylist** (SEC-2, GAP-7): enrollment token, private key, license blob, storage credentials, full file paths/usernames are never logged in clear; a single redaction middleware enforces it so a field can never accidentally leak.

#### Binary signing (UPD-1)

- **Authenticode-sign** `beyz-backup-agent.exe`, `beyz-backup-updater.exe`, and the Inno Setup installer (`signtool`, EV cert preferred). This is OS-level trust and is **separate** from the Ed25519 update-manifest signature (which is the in-product update trust root). Both are present from the first ship.

#### Explicitly DEFERRED in Sprint 1 — and why acceptable

| Deferred | To | Why acceptable in Sprint 1 |
|---|---|---|
| **mTLS** | Sprint 8 | SPKI pinning + token + TLS 1.2/1.3 closes the MITM gap; the enrollment-issued cert is stored *now* as a client credential so promotion to mTLS needs **no enrollment-format change** (SEC-4). |
| **Production HSM / offline signing ceremony** | Sprint 8 | Verification path is real and enforcing against an **embedded test public key** today; only the *private-key custody* is deferred, which the agent never sees (SEC-3/UPD-1). |
| **Ransomware detection, immutable/WORM storage** | Sprint 10 / Sprint 7 | No backup data exists yet in Sprint 1; nothing to protect at rest (PRD scope). |
| **Central audit store + WORM shipping** | Sprint 8 | Event **schema + hash-chain is frozen now**; Sprint 8 adds transport only (ARCH-9). |
| **Crypto engine / key hierarchy code** | Sprint 4 | Enrollment payload + `config.yaml` **reserve** `wrapped_device_key`, `key_wrap_version`, `key_id` fields now (SEC-1/RST-1/BKP-8) so the on-disk format needs no breaking change. |

#### IRREVERSIBLE decisions that MUST be made in Sprint 1

These are "one-line-now / impossible-later" choices because the first installed agent and the first wire contract bake them in:

1. **`/v1/` in every path + `X-Agent-Version` / `X-Protocol-Version` headers on every request**, with server `426 Upgrade Required` below the floor (ARCH-2, SEC-8, BKP-7, SCALE-9). Compatibility window (server supports agents N minor versions back) documented in `SECURITY.md`/`ARCHITECTURE.md`.
2. **Ed25519 update-signing algorithm + embedded pinned key-SET + manifest `key_id` + (possibly empty) revocation-list field** (UPD-1/2). Unrotatable if shipped as a single key.
3. **`tenant_id` (+ MSP `parent_org_id`) embedded immutably in the agent cert at enrollment**; every request authorized against that binding (ARCH-6, LIC-6, SCALE-4).
4. **Server-issued opaque `device_id` as primary identity**, hardware signals advisory only (LIC-2, SEC-5).
5. **Reserved key/recovery fields** in the enrollment payload and `config.yaml` (`wrapped_device_key`, `key_wrap_version`, `key_id`) and a **"recovery key downloaded/confirmed" gate** placeholder (SEC-1, RST-1, BKP-8).
6. **Frozen security-event schema with hash chaining** (ARCH-9, SEC-7).
7. **OpenAPI 3 spec as the source of truth** for the four agent endpoints, committed to the repo (ARCH-1) — the agent client is generated from it, not hand-rolled.
8. **Anti-rollback semantics** (signed monotonic version + floor) in the manifest format (UPD-6).
9. **SPKI pin set + documented pin-rotation** for the control channel (SEC-4).

---

### Test Strategy

Tests map to acceptance themes: **A. Config & Identity**, **B. Enrollment**, **C. Heartbeat/Poll**, **D. Updater & Rollback**, **E. Service Lifecycle**, **F. Installer**, **G. Security Negatives**. Frameworks: Go std `testing` + `testify` (assertions/mocks), `httptest` for the mock SaaS, **Prism** (`stoplight/prism`) and/or **Schemathesis** as the contract-test server bound to the committed OpenAPI 3 spec (ARCH-1), Pester for Windows-Service/installer PowerShell checks, GitHub Actions for the CI matrix.

#### Contract-first foundation (ARCH-1)

The OpenAPI 3 spec for `POST /v1/enroll`, `POST /v1/agents/{id}/register`, `POST /v1/agents/{id}/heartbeat`, `GET /v1/agents/{id}/tasks` is the **source of truth**. The agent HTTP client is generated from it (`oapi-codegen`). **Prism mock server** serves the spec for integration tests; **Schemathesis** fuzzes the agent's request shapes against the spec. Sprint 2 implements the same artifact, so the Sprint 1 placeholder validates against what Sprint 2 will build.

#### Unit tests

| Area | Cases | Maps to |
|---|---|---|
| **Config parsing & precedence** | valid/invalid YAML; missing required keys → clear error (not panic); **precedence order** (CLI flag > env > `config.yaml` > built-in default) verified deterministically; unknown fields **ignored** (forward-compat envelope, SCALE-9); secret fields absent from parsed-and-logged struct | A |
| **Fingerprint / device GUID** | GUID generated once and **persisted/stable** across restarts; regenerated only if `state\device.json` absent; hardware signals collected but not used as primary key; no PII in fingerprint | A |
| **Update manifest signature verify** | **happy path** (valid Ed25519 over canonical manifest → accept); **tampered manifest** (flip a byte → reject); **wrong key_id / unknown key → reject**; **revoked key → reject**; **binary hash mismatch vs manifest → reject**; **anti-rollback** (version ≤ current → reject; emergency-downgrade flag w/ root sig → accept) | D, G |
| **Canonical JSON** | re-serialization is deterministic/byte-stable (signature must cover canonical form) | D |
| **Backoff / jitter** | exponential backoff with **full jitter**; decorrelated jitter on reconnect; **max interval cap**; server-returned `next_poll_seconds` overrides default (SCALE-1) | C |
| **DPAPI wrap/unwrap** | round-trip wrap→unwrap of key bytes; unwrap of tampered blob fails closed | A, G |
| **Log redaction** | token / key / license / paths never appear in emitted JSON | G |
| **Hash-chain** | `seq`/`prev_hash` chain links correctly; a removed/edited record breaks verification | G |

#### Integration tests

- **Enroll → register → heartbeat → poll** against the **Prism mock** bound to the OpenAPI spec: agent enrolls with a test token, stores cert+`device_id`, sends a heartbeat carrying `X-Agent-Version`/`X-Protocol-Version`, polls tasks (placeholder empty list). Assert presence semantics and that the heartbeat payload is **minimal** (fingerprint + version + status enum), separate from the poll response (SCALE-2).
- **Token single-use:** second enroll with a consumed token → mock returns `409`; agent surfaces a clear error and does not overwrite valid state (B, G).
- **Server-controlled cadence:** mock returns `next_poll_seconds`; agent honors it with jitter (C, SCALE-1).
- **Version floor:** mock returns `426`; agent stops calling and logs `update_required` (ARCH-2/SEC-8).
- **Updater rollback with a fake bad update:** mock serves a **validly signed** manifest+binary whose binary intentionally fails the post-start health gate (exits, never heartbeats). Assert: updater stages → verifies → swaps → starts → **health gate times out at 90s → restores `.exe.bak` + prior config → restarts → emits `update.rolled_back`** (UPD-4). Second variant: **bad signature** manifest → updater never swaps (D, G).
- **TOCTOU re-verify:** binary swapped on disk between download and exec → re-verify of exact bytes catches it (SEC-9).

#### Windows Service lifecycle tests (Pester / `sc.exe`)

`install → start → stop → restart → uninstall`, asserting service state at each step, that the service runs as `LocalSystem`, auto-start is configured, and **service recovery** (restart-on-failure) is set. Crash-restart test: kill the process, confirm SCM restarts it and state files survive.

#### Installer tests (Inno Setup)

- **Silent install with token:** `setup.exe /VERYSILENT /ENROLLTOKEN=<test>` → folders created, **ACLs verified** (`icacls` shows `Users` removed from `state\`), service registered + started, agent enrolled against mock (F).
- **Interactive token dialog** path (PRD req 18).
- **Token not in `/LOG`:** assert the installer log file does **not** contain the token value (SEC-2, G).
- **Uninstall / decommission** (GAP-4): service stopped, secrets in `state\` securely removed, best-effort de-enroll call made; verify no key/cert/license left behind.

#### Security test cases (theme G — explicit)

- Tampered update manifest **rejected**; bad/unknown/revoked signature **rejected**; binary-hash-mismatch **rejected**; downgrade **rejected** (anti-rollback).
- **Token replay rejected** (server `409`, atomic consumption).
- **Secrets never logged** — grep agent + installer logs for token/key/license substrings → must be empty.
- **Config/state ACLs** — automated `icacls` assertion that `Users` cannot read `state\`.
- **TLS pin enforced** — point the agent at a server presenting a non-pinned cert (self-signed via `httptest` with a different SPKI) → connection **refused**; confirm system-CA-only trust is rejected for the control channel (SEC-4).
- **Cloned-state detection** — copy `state\` to a second identity and enroll → server-side mismatch detectable (LIC-2 heuristic, mock-asserted).

#### CI matrix & coverage

```yaml
matrix:
  os: [windows-latest, ubuntu-latest]   # Windows primary; Linux = systemd build/prep only
  go:  ['1.22','1.23']
steps: [build agent+updater, unit, contract (schemathesis vs openapi), gitleaks, race detector]
```

- **Windows-only:** service-lifecycle (Pester) + installer (silent) jobs.
- **Linux:** build + unit + contract only (no service install; systemd unit lint).
- **Gating:** `gitleaks` and the **manifest-signature negative tests** are **required** checks (no merge on failure) — they enforce the "no placeholder security" and "never remove update signature validation" rules from `CLAUDE.md`.
- **Coverage targets:** **≥85% on security-critical packages** (signature verify, enrollment, DPAPI, config precedence, redaction, hash-chain); **≥70% overall**. Updater swap/rollback paths must have explicit happy+failure coverage (not just line coverage).

#### Manual test instructions (PRD reqs 22, 120 — heartbeat & enrollment)

1. **Stand up the mock SaaS:** `prism mock openapi/agent-v1.yaml` (or run the committed `httptest` stub) on `https://localhost:8443` with the test SPKI the agent pins.
2. **Issue a test enrollment token** from the mock's `/v1/enroll` seed (single-use, 30-min TTL).
3. **Silent install:** `setup.exe /VERYSILENT /ENROLLTOKEN=<token>`; or install then run `beyz-backup-agent.exe enroll` and paste the token at the prompt (stdin).
4. **Verify enrollment:** confirm `state\agent.crt` and `device.json` exist; check `logs\` for `enroll.success` with the `device_id` (token value must be absent/redacted).
5. **Verify heartbeat:** watch the mock log for `POST /v1/agents/{id}/heartbeat` arriving on cadence with jitter, carrying `X-Agent-Version`/`X-Protocol-Version`; or tail `logs\` for `heartbeat.ok`.
6. **Verify poll:** confirm `GET /v1/agents/{id}/tasks` returns the empty placeholder list and the agent honors `next_poll_seconds`.
7. **Replay check:** re-run `enroll` with the same token → expect a clear "token already used" error.
8. **Pinning check:** point `server.base_url` at a server with a different cert → expect a refused connection.

---

Relevant files (all under `/Users/coosef/Documents/cloude/backup/`): `CLAUDE.md`, `docs/PRD.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY.md`, `docs/ROADMAP.md`. This section recommends back-edits to `docs/SECURITY.md` (reconciled secret-storage rule, token lifecycle, fingerprint scheme, TLS-pinning + rotation, audit-event schema), `docs/ARCHITECTURE.md` (protocol versioning + compatibility window, tenant isolation), and `CLAUDE.md` (clarify the env-var rule to "no secrets in source/config"), plus a new committed `openapi/agent-v1.yaml` as the contract source of truth.

---
