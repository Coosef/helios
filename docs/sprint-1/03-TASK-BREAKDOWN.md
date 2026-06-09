# Beyz Backup — Sprint 1 Task Breakdown

> Small, sequenced implementation tasks for the Agent Foundation. Complexity: XS (<½d) · S (½–1d) · M (1–2d) · L (2–4d) · XL (split before starting). Priority: P0 = foundation/blocking · P1 = core sprint goal · P2 = hardening/stretch.

**36 tasks.** P0: 30 · P1: 5 · P2: 1

## Scope cut-line (read first)

The completeness review judged the *fully elaborated* design to be ~2–3 sprints. To fit one sprint while keeping security enforcing (CLAUDE.md), apply the **§0.6 updater cut** and treat the following as **stretch / first-to-cut** if time runs short — *without* touching the enforcing security paths:

- Persistent **second watchdog service** and the **two-stage updater self-update bootstrap** → defer to Sprint 8 (ship on-demand updater + `MOVEFILE_DELAY_UNTIL_REBOOT` stub).
- **Config hot-reload** → restart-to-apply is acceptable for Sprint 1.
- **Long-poll / `work_available` fast-path execution** → keep the reserved fields, defer the implementation.
- **Schemathesis fuzzing**, **Linux service install tests** → keep build+unit+Prism-contract; defer the rest.

**Never cut:** Ed25519 signature verification, anti-rollback, hash checks, ACL lockdown, secret redaction, single-use-token handling, TLS pinning — these are the must-haves CLAUDE.md forbids stubbing.

## Task table

| ID | Title | Component | Prio | Cplx | Depends on |
|----|-------|-----------|------|------|------------|
| S1-T01 | Go module scaffold, Clean Architecture folder tree, Taskfile | tooling | P0 | S | — |
| S1-T02 | OpenAPI 3.1 control-plane spec (source of truth) for the 6 agent endpoints | protocol | P0 | M | S1-T01 |
| S1-T03 | Generate pkg/proto wire types + typed client from OpenAPI via oapi-codegen | protocol | P0 | S | S1-T02 |
| S1-T04 | pkg/wireversion: agent/protocol version constants + 426 handling + version headers | shared | P0 | S | S1-T01 |
| S1-T05 | pkg/hashing: algorithm-tagged digests (blake3:/sha256:) with dispatch-on-tag verify | shared | P0 | S | S1-T01 |
| S1-T07 | Config subsystem: config.yaml load, JSON-Schema validate, typed immutable struct | agent | P0 | M | S1-T01 |
| S1-T08 | Structured logging (zerolog JSON) + rotation + redaction + dual streams | agent | P0 | M | S1-T01 |
| S1-T09 | internal/agent/audit: frozen hash-chained security-event schema emitter | agent | P0 | M | S1-T08, S1-T05 |
| S1-T10 | internal/agent/state: machine-protected state store (bbolt) + DPAPI value-wrapping + atomic writes | agent | P0 | L | S1-T01 |
| S1-T11 | internal/agent/identity: device GUID, local keypair + CSR, SPKI-thumbprint fingerprint | agent | P0 | M | S1-T10 |
| S1-T12 | Hardened HTTP client: TLS 1.2+/1.3, SPKI pinning, keep-alive, proxy, IPv6, backoff+jitter | agent | P0 | L | S1-T04, S1-T07, S1-T08 |
| S1-T13 | internal/transport/saasclient: typed client over pkg/proto (Enroll/Register/Heartbeat/PollTasks/Ack/Status) | agent | P0 | M | S1-T03, S1-T12 |
| S1-T14 | internal/agent/enroll: token consume -> CSR submit -> persist cert/device_id/license; idempotent re-enroll | agent | P0 | L | S1-T11, S1-T13, S1-T09 |
| S1-T15 | internal/agent/heartbeat: minimal presence payload, server-directed jittered cadence, update-ack channel | agent | P0 | M | S1-T14 |
| S1-T16 | internal/agent/tasks: task-poll placeholder with lease/ack/status + forward-compat envelope | agent | P0 | M | S1-T15 |
| S1-T18 | internal/agent/service: OS service lifecycle adapter (Windows SCM + Linux systemd prep) | agent | P0 | M | S1-T01 |
| S1-T19 | internal/agent/app + cmd/agent: DI composition root and run loop | agent | P0 | M | S1-T07, S1-T08, S1-T10, S1-T16, S1-T17, S1-T18 |
| S1-T21 | pkg/manifest + api/manifest.schema.json: update-manifest struct, JCS canonical serialization, schema | protocol | P0 | M | S1-T01 |
| S1-T22 | internal/updater/trust + embedded Ed25519 test public-key set with key-ids + revocation list | updater | P0 | S | S1-T01 |
| S1-T23 | internal/updater/verify: REAL Ed25519 manifest verification + BLAKE3/SHA256 binary hash (enforcing) | updater | P0 | M | S1-T21, S1-T22, S1-T05 |
| S1-T24 | internal/updater/manifestcheck: fetch signed manifest from pinned endpoint + anti-rollback decision | updater | P0 | M | S1-T23, S1-T12 |
| S1-T25 | internal/updater/swap: staged current/new/backup atomic MoveFileEx replace of the agent binary | updater | P0 | M | S1-T18, S1-T10 |
| S1-T26 | internal/updater/healthgate + rollback: 90s heartbeat-ack gate and integrity-checked restore | updater | P0 | M | S1-T25, S1-T15 |
| S1-T27 | internal/updater/app + cmd/updater: persisted FSM orchestrator, on-demand updater binary (no service) | updater | P0 | M | S1-T24, S1-T26, S1-T18 |
| S1-T28 | Prism/Schemathesis mock SaaS + static update manifest fixtures for CI | tooling | P0 | M | S1-T02, S1-T21 |
| S1-T29 | Inno Setup installer: dual-binary install, ACL-locked folders, single (agent) service register/start, token input | installer | P0 | L | S1-T19, S1-T27 |
| S1-T31 | CI pipeline: vet/lint/gosec, unit+contract tests, OpenAPI drift, gitleaks, race, Inno build | tooling | P0 | M | S1-T28 |
| S1-T32 | Security-controls integration + negative-test task and gating | shared | P0 | M | S1-T12, S1-T14, S1-T23, S1-T24, S1-T29, S1-T10 |
| S1-T33 | Unit test suite for shared kernel + agent core packages | shared | P0 | M | S1-T07, S1-T09, S1-T11, S1-T12, S1-T21, S1-T23 |
| S1-T34 | Integration tests: enroll->register->heartbeat->poll vs mock + updater state-machine end-to-end | shared | P0 | M | S1-T16, S1-T27, S1-T28 |
| S1-T17 | internal/agent/license: load + verify server-signed license blob against embedded public key (advisory) | agent | P1 | S | S1-T10, S1-T09 |
| S1-T20 | build/linux systemd unit + cross-platform service/state path abstraction | agent | P1 | S | S1-T10, S1-T18 |
| S1-T30 | Windows version-info/Authenticode signing hooks + Taskfile build/cross/dist | tooling | P1 | S | S1-T19, S1-T27 |
| S1-T35 | Windows service-lifecycle + installer Pester tests | installer | P1 | M | S1-T29, S1-T19, S1-T27 |
| S1-T36 | Six design records (DR-01..06) + docs: README, INSTALL, ARCHITECTURE/SECURITY/ROADMAP updates | docs | P1 | M | S1-T32, S1-T29, S1-T27, S1-T14 |
| S1-T06 | pkg/storage StorageBackend interface + Capabilities + local no-op stub | shared | P2 | XS | S1-T01 |

## Task detail

### S1-T01 — Go module scaffold, Clean Architecture folder tree, Taskfile
`tooling · P0 · complexity S · depends on: none`

Create the single Go module monorepo (module github.com/beyzbackup/beyz-backup, Go 1.23.x pinned in go.mod). Lay down the full directory tree per the design: cmd/{agent,updater}, internal/{agent/*,updater/*,transport/*}, pkg/{proto,wireversion,manifest,hashing,storage}, api/, configs/, build/{windows,linux,keys}, installer/, docs/, test/. Add empty package doc.go stubs so the tree compiles. Author Taskfile.yml with targets build, cross, lint, test, contract, dist, installer (most can be stubs that echo+exit 0 initially). Covers PRD 1, 19, 21.

**Done when:** go build ./... succeeds on the empty scaffold; `task --list` shows all targets; folder tree matches the design doc.

### S1-T02 — OpenAPI 3.1 control-plane spec (source of truth) for the 6 agent endpoints
`protocol · P0 · complexity M · depends on: S1-T01`

Author api/openapi.yaml (OpenAPI 3.1) as the committed source of truth (ARCH-1): POST /v1/enroll, POST /v1/agents/{id}/register, POST /v1/agents/{id}/heartbeat, GET /v1/agents/{id}/tasks, POST .../tasks/{id}/ack, POST .../tasks/{id}/status. Model all request/response schemas including reserved-but-null fields (wrapped_device_key, key_id, key_wrap_version, tenant_id, parent_account_id, license_blob, license_state, reported_usage_bytes, supported_format_versions, cadence next_poll/next_heartbeat/jitter, work_available, hold_seconds, server_time). Define version headers X-Agent-Version/X-Protocol-Version, 426 Upgrade Required, RFC 9457 problem+json error body, Idempotency-Key. Freeze the placeholder task envelope (task_id, type noop|config_refresh|update_check, schema_version, lease_seconds, sequence). Covers PRD 8 (contract), and the ARCH-1/ARCH-2/SCALE-9 forward-compat decisions.

**Done when:** Spec lints clean (spectral/openapi validator); every reserved field present; `prism mock api/openapi.yaml` boots and serves all 6 paths.

### S1-T03 — Generate pkg/proto wire types + typed client from OpenAPI via oapi-codegen
`protocol · P0 · complexity S · depends on: S1-T02`

Wire oapi-codegen into the build (Taskfile `gen` target + go:generate) to emit pkg/proto request/response structs and a typed HTTP client from api/openapi.yaml. Add a CI drift check that regenerates and fails on diff (enforces 'spec is source of truth, not Go code'). Hand-write nothing the generator can produce. Covers part of PRD 8/19.

**Done when:** `task gen` produces pkg/proto/*.gen.go; re-running gen yields no diff; generated package compiles.

### S1-T04 — pkg/wireversion: agent/protocol version constants + 426 handling + version headers
`shared · P0 · complexity S · depends on: S1-T01`

Implement pkg/wireversion holding AgentVersion, ProtocolVersion, MinSupportedProtocol constants and helpers to set X-Agent-Version/X-Protocol-Version on outbound requests and to detect/parse a 426 Upgrade Required response (with min_supported_version body). Version string is injected at build time via ldflags (define the variable + Taskfile flag). Covers the ARCH-2/SEC-8 versioning decision baked into every request.

**Done when:** Unit test: headers set correctly; 426 detector returns the parsed floor; ldflags override AgentVersion at build.

### S1-T05 — pkg/hashing: algorithm-tagged digests (blake3:/sha256:) with dispatch-on-tag verify
`shared · P0 · complexity S · depends on: S1-T01`

Implement pkg/hashing producing and verifying tagged digests 'blake3:<hex>' and 'sha256:<hex>' using zeebo/blake3 (or lukechampine.com/blake3) and stdlib crypto/sha256. Verify dispatches on the tag prefix. BLAKE3 is the default content hash; SHA256 is the update-binary path per CLAUDE.md/SECURITY.md. Provide streaming hashers for large files. Covers the hashing technical decision (CLAUDE.md) used by updater + audit chain.

**Done when:** Test vectors: known input -> known blake3:/sha256: tag; verify accepts matching, rejects mismatched and unknown-algo tags.

### S1-T07 — Config subsystem: config.yaml load, JSON-Schema validate, typed immutable struct
`agent · P0 · complexity M · depends on: S1-T01`

Implement internal/agent/config: load C:\ProgramData\BeyzBackup\config.yaml (koanf+yaml.v3), validate against configs/config.schema.json (santhosh-tekuri/jsonschema v6), and expose an immutable typed struct. Author configs/config.sample.yaml (annotated, Sprint 1 keys + reserved crypto/schedule/throttle/storage placeholders) and configs/config.schema.json. Implement defaults<file<env(BEYZ_)<CLI precedence. Fail-closed on invalid/missing config with a non-zero exit and actionable message. Enforce https-only server.url, >=1 spki_pin, tls_min>=1.2, interval bounds. Ignore reserved-unknown fields (forward-compat). Covers PRD 5 and the GAP-1/GAP-6/RST-1 reserved-field decisions.

**Done when:** Unit tests: valid loads, invalid/missing fail with non-zero + message, precedence order deterministic, unknown reserved keys ignored, http:// server.url rejected.

### S1-T08 — Structured logging (zerolog JSON) + rotation + redaction + dual streams
`agent · P0 · complexity M · depends on: S1-T01`

Implement the logging layer on rs/zerolog: JSON one-event-per-line to logs\agent.log with lumberjack rotation (max_size_mb/backups/age), fixed-field context (tenant_id, device_id, agent_version, component, event, trace_id, ts RFC3339, outcome). Add a typed Secret wrapper whose String()/MarshalJSON() returns ***REDACTED*** and a redaction middleware so tokens/keys/license/Authorization headers/full paths can never leak. Provide a second security stream writer for logs\security.log. Behind a small Logger interface (DI). Covers PRD 6 and the SEC-2/GAP-7 redaction rules.

**Done when:** Unit tests: emitted records are valid JSON with required fields; Secret-wrapped values render REDACTED; rotation writer configured; grep for token/key substrings in output is empty.

### S1-T09 — internal/agent/audit: frozen hash-chained security-event schema emitter
`agent · P0 · complexity M · depends on: S1-T08, S1-T05`

Implement internal/agent/audit emitting the frozen audit schema {schema_version, seq, prev_hash, ts_local, ts_server(null), event_type, tenant_id, device_id, actor, outcome, detail} with per-record BLAKE3 hash chaining (this_hash = blake3(prev_hash || canonical(event))). Define the controlled event_type vocabulary (enroll.*, update.*, auth.failure, license.*, config.tamper, service.*). Writes to security stream + mirrors to the bbolt audit-spool bucket. Transport to SaaS is stubbed (Sprint 8) but schema is final now. Covers ARCH-9/SEC-7/GAP-7.

**Done when:** Unit tests: chain links verify; editing/removing a record breaks verification; canonical serialization is deterministic; event_type vocabulary enforced.

### S1-T10 — internal/agent/state: machine-protected state store (bbolt) + DPAPI value-wrapping + atomic writes
`agent · P0 · complexity L · depends on: S1-T01`

Implement internal/agent/state as a bbolt single-file store (state\agent-state.db) with buckets identity/credentials/runtime/updater/audit-spool per the frozen record set, plus the separate write-once state\device.guid file. DPAPI machine-scope wrap (CRYPTPROTECT_LOCAL_MACHINE via x/sys/windows or billgraziano/dpapi) for agent_private_key/license_blob/wrapped_device_key inside bbolt; non-secret values plaintext. All writes atomic (bbolt Update; write-temp->fsync->rename for sidecar files). Linux fallback: root-only 0600 behind the same interface (DPAPI is a build-tagged adapter). Covers the SEC-6/ARCH-8/STO-1 secret-store decision underpinning enrollment.

**Done when:** Unit/integration tests: DPAPI wrap->unwrap round-trips on Windows; tampered blob fails closed; device.guid persists across reopen; atomic write survives simulated mid-write.

### S1-T11 — internal/agent/identity: device GUID, local keypair + CSR, SPKI-thumbprint fingerprint
`agent · P0 · complexity M · depends on: S1-T10`

Implement internal/agent/identity: generate/persist a random UUIDv4 device GUID (survives reinstall), generate the agent keypair locally (crypto/ecdsa or ed25519 + crypto/x509), build a CSR (SEC-5), and compute the fingerprint = SPKI thumbprint XOR-combined with the device GUID. Collect advisory-only hardware signals (Windows MachineGuid, disk serial, NIC MAC) for clone-detection — never as the primary key (LIC-2). No PII in the fingerprint. Covers PRD 7 (token/identity groundwork) and ARCH-7/SEC-5.

**Done when:** Unit tests: GUID generated once and stable across restarts; CSR is valid PEM with the local public key; fingerprint deterministic; no PII fields present.

### S1-T12 — Hardened HTTP client: TLS 1.2+/1.3, SPKI pinning, keep-alive, proxy, IPv6, backoff+jitter
`agent · P0 · complexity L · depends on: S1-T04, S1-T07, S1-T08`

Implement internal/transport/httpclient on stdlib net/http with a custom tls.Config (MinVersion TLS1.2, prefer 1.3) and SPKI public-key pinning via VerifyConnection/VerifyPeerCertificate against the pin set from config — reject system-CA-only trust for the control channel (SEC-4). Mandatory keep-alive/connection pooling (MaxIdleConnsPerHost), system+explicit proxy support, IPv6, and exponential backoff with full/decorrelated jitter (cenkalti/backoff/v4) capped at backoff_max. Inject version headers. Authorization header redacted in any logging middleware. Covers SEC-4/GAP-5/GAP-8/SCALE-1/SCALE-3.

**Done when:** Unit tests via httptest: connection to a non-pinned cert is refused; pinned cert accepted; backoff produces capped jittered intervals; proxy + version headers applied.

### S1-T13 — internal/transport/saasclient: typed client over pkg/proto (Enroll/Register/Heartbeat/PollTasks/Ack/Status)
`agent · P0 · complexity M · depends on: S1-T03, S1-T12`

Implement internal/transport/saasclient as the only package that knows endpoint paths: wraps the generated client + httpclient to expose Enroll, Register, Heartbeat, PollTasks, AckTask, ReportStatus. Maps 426 to a typed UpgradeRequired error and RFC 9457 problem+json bodies to typed errors; attaches Idempotency-Key on mutating calls; records server_time for clock-skew detection (>300s warning). Covers PRD 8 plumbing + the ARCH-1 contract boundary.

**Done when:** Contract tests against Prism mock: each method round-trips; 426 and problem+json map to typed errors; Idempotency-Key sent; skew warning fires on shifted server_time.

### S1-T14 — internal/agent/enroll: token consume -> CSR submit -> persist cert/device_id/license; idempotent re-enroll
`agent · P0 · complexity L · depends on: S1-T11, S1-T13, S1-T09`

Implement the enrollment use-case state machine (UNENROLLED->ENROLLING->ENROLLED->DEGRADED->DECOMMISSIONED, persisted): read the one-shot token (stdin/secure-file/dialog input, never persisted in config), POST /v1/enroll with CSR+fingerprint, on 201 atomically persist agent_id/cert/tenant binding/license to the state store, then POST /v1/register to confirm. Handle 401/409(replay, security-logged, no silent re-enroll)/422/426/429/5xx per the contract. Emit enroll.attempt/success/fail audit events; zero the token from memory after use. Boots into the correct persisted state after restart. Covers PRD 7, 8 and SEC-2/GAP-2.

**Done when:** Integration test vs mock: fresh token -> ENROLLED + persisted cert/device_id; consumed token -> 409 surfaced, valid state not overwritten; token never appears in logs; state survives restart.

### S1-T15 — internal/agent/heartbeat: minimal presence payload, server-directed jittered cadence, update-ack channel
`agent · P0 · complexity M · depends on: S1-T14`

Implement internal/agent/heartbeat: build the minimal presence payload (agent_version, status enum idle|backing_up|restoring|updating|error, health block service_state/last_error/disk_free_pct/connectivity) plus reserved license_state/reported_usage_bytes/update_result. Send POST /v1/agents/{id}/heartbeat on the server-returned next_heartbeat_seconds with mandatory +/- jitter (config fallback floor only before first response); apply work_available (poll soon) and 401(->DEGRADED/re-enroll)/426(->updater)/5xx(backoff). Carry update_result:"ok" on the first post-update heartbeat (UPD-4 rollback signal). Covers PRD 9 and SCALE-1/SCALE-2/LIC-1.

**Done when:** Integration test vs mock: heartbeat arrives on jittered cadence honoring next_heartbeat_seconds; payload is minimal; 426 stops the loop; work_available triggers an immediate poll.

### S1-T16 — internal/agent/tasks: task-poll placeholder with lease/ack/status + forward-compat envelope
`agent · P0 · complexity M · depends on: S1-T15`

Implement internal/agent/tasks: GET /v1/agents/{id}/tasks on the server-supplied jittered next_poll_seconds and immediately when heartbeat returns work_available. Parse the versioned task envelope, handle reserved work_available/rollout/update_check fields and unknown fields without error (SCALE-9), log 'no tasks', and no-op for Sprint 1 (mock returns []). Wire the lease/ack/status round-trip (POST ack with Idempotency-Key, POST status accepted|in_progress|succeeded|failed|rolled_back) and a small seen-task dedup set + monotonic sequence ordering — exercised against the mock noop task. Covers PRD 10 and SCALE-3/UPD-5.

**Done when:** Integration test vs mock: empty list -> 'no tasks' logged + honors next_poll_seconds; injected noop task -> ack->status round-trips idempotently; unknown envelope fields ignored.

### S1-T18 — internal/agent/service: OS service lifecycle adapter (Windows SCM + Linux systemd prep)
`agent · P0 · complexity M · depends on: S1-T01`

Implement internal/agent/service as a thin interface over kardianos/service abstracting install/start/stop/shutdown/restart for Windows SCM and Linux systemd, with graceful shutdown, panic recovery, and a single-instance guard. Support console mode for dev. Keep the interface thin so raw x/sys/windows/svc can replace it later. Covers PRD 3 and part of PRD 4.

**Done when:** Manual/Pester: sc query BeyzBackupAgent reports RUNNING after install+start; stop/restart work; console mode runs in foreground; second instance is rejected.

### S1-T19 — internal/agent/app + cmd/agent: DI composition root and run loop
`agent · P0 · complexity M · depends on: S1-T07, S1-T08, S1-T10, S1-T16, S1-T17, S1-T18`

Implement internal/agent/app (constructor-based DI wiring: config->logger->state->httpclient->saasclient->enroll/heartbeat/tasks/license services + run loop) and cmd/agent (parse flags, select service vs console vs one-shot `enroll` subcommand, hand off to app). Boots into the persisted enrollment state; idles cleanly when UNENROLLED (no crash). Produces beyz-backup-agent.exe. Covers PRD 2 and ties together 7-10.

**Done when:** beyz-backup-agent.exe builds and runs; unenrolled start idles without crashing; enrolled start runs heartbeat+poll loops; `enroll` subcommand performs first-run enrollment from stdin.

### S1-T21 — pkg/manifest + api/manifest.schema.json: update-manifest struct, JCS canonical serialization, schema
`protocol · P0 · complexity M · depends on: S1-T01`

Implement pkg/manifest (update-manifest struct matching the frozen format: schema_version, protocol_version, key_id, channel, target_version, min_supported_version, released_at, rollout{cohort_percent,update_allowed,pin_version}, key_revocation_list, artifacts[{platform,component,url,size_bytes,hash_algo,hash,sha256}], signature{algo,key_id,value}) with RFC 8785 JCS deterministic canonical serialization (signature.value emptied for signing). Author api/manifest.schema.json (committed, versioned) and docs/format/manifest-envelope.md reserved backup-manifest envelope. Covers the UPD-7/BKP-3 frozen-format decision.

**Done when:** Unit tests: round-trip serialize is byte-stable (JCS); sample manifest validates against manifest.schema.json; emptying signature for signing is deterministic.

### S1-T22 — internal/updater/trust + embedded Ed25519 test public-key set with key-ids + revocation list
`updater · P0 · complexity S · depends on: S1-T01`

Implement internal/updater/trust holding the compile-time embedded public key-SET keyed by key_id plus an (empty) revocation list — the trust anchor (UPD-1/2, ARCH-4). Generate build/keys/update_pub_test.pem (TEST public key only; private key never in repo/CI), embed via go:embed. Document offline/HSM private-key custody in DR-03. Covers the irreversible Ed25519 embedded-key-set decision.

**Done when:** Trust set loads the embedded test pubkey by key_id at init; unknown/revoked key_id lookups fail; no private key material anywhere in the repo.

### S1-T23 — internal/updater/verify: REAL Ed25519 manifest verification + BLAKE3/SHA256 binary hash (enforcing)
`updater · P0 · complexity M · depends on: S1-T21, S1-T22, S1-T05`

Implement internal/updater/verify with REAL enforcing logic (never return true, ARCH-4/SEC-3): verify the Ed25519 signature over the JCS-canonical manifest using the trust key-set (reject unknown/revoked key_id, tampered manifest); then verify the staged binary's BLAKE3 (and SHA256 cross-check) against the hash taken ONLY from the now-trusted manifest; verify on the same locked file handle that will be executed (close TOCTOU, SEC-9). The hash must never come from any source other than the signed manifest (UPD-8). Covers PRD 12 (signature verification + SHA256 validation) — real architecture.

**Done when:** Unit tests: valid sig accepts; flipped byte/wrong key_id/revoked key reject; binary-hash mismatch rejects; hash sourced only from verified manifest; verify uses the locked handle.

### S1-T24 — internal/updater/manifestcheck: fetch signed manifest from pinned endpoint + anti-rollback decision
`updater · P0 · complexity M · depends on: S1-T23, S1-T12`

Implement internal/updater/manifestcheck: fetch the manifest over the pinned-cert HTTPS endpoint (reuse httpclient), reject below-floor protocol_version, and make the anti-rollback decision — reject target_version <= current_version (read from state) unless a separately-signed emergency-downgrade flag is present; comparison uses the signed manifest version, never a filename/header (UPD-6). Apply the rollout gate (update_allowed + in-cohort) so the agent only updates when told (UPD-5). Sprint 1 remote is a static test manifest served by the mock. Covers PRD 12 (manifest check) and UPD-5/6/SEC-9.

**Done when:** Unit tests: downgrade (<=current) rejected; emergency-signed downgrade accepted; update_allowed=false or out-of-cohort -> no update; below-floor protocol_version rejected.

### S1-T25 — internal/updater/swap: staged current/new/backup atomic MoveFileEx replace of the agent binary
`updater · P0 · complexity L · depends on: S1-T18, S1-T10`

Implement internal/updater/swap: the staging\ layout (download.tmp, .exe.new, .exe.bak), stop BeyzBackupAgent via SCM (bounded timeout, kill if hung), snapshot config+current binary as a consistent (binary,config) pair, atomic replace of the AGENT binary via MoveFileEx(MOVEFILE_REPLACE_EXISTING|WRITE_THROUGH), restart service. Integrity-check the .bak before any restore. Updater self-update (two-stage bootstrap) is OUT of Sprint 1 scope — deferred to Sprint 8 per §0.6; the updater binary itself is updated via the installer. Covers PRD 12 (stop service, replace binary, restart) and UPD-3.

**Done when:** Integration test (fixtures): stage->stop->swap->restart sequence runs against the agent binary; interrupted swap leaves old OR new binary intact (never truncated); no updater self-update code is present (deferred to Sprint 8).

### S1-T26 — internal/updater/healthgate + rollback: 90s heartbeat-ack gate and integrity-checked restore
`updater · P0 · complexity M · depends on: S1-T25, S1-T15`

Implement internal/updater/healthgate: after restart, require the new agent to (a) reach RUNNING and (b) self-report update_result:"ok" via heartbeat (state\health.json + service state, both true) within 90s, else trigger rollback. Implement rollback: stop failed agent, BLAKE3-integrity-check the .bak before restoring, MoveFileEx restore the paired (binary,config), restart, emit update.rolled_back audit + report failure to SaaS. Triggers: start failure, gate timeout, crash-loop, updater-died-mid-swap (recovered from persisted FSM). Covers PRD 12 (rollback) and UPD-4/9.

**Done when:** Integration test: a fixture binary that never heartbeats -> gate times out at 90s (compressed in test) -> .bak integrity-checked -> restored -> restart -> update.rolled_back emitted.

### S1-T27 — internal/updater/app + cmd/updater: persisted FSM orchestrator, on-demand updater binary (no service)
`updater · P0 · complexity M · depends on: S1-T24, S1-T26, S1-T18`

Implement internal/updater/app orchestrating the durable FSM (IDLE->MANIFEST_VERIFIED->DOWNLOADING->STAGED->STOPPING_AGENT->BACKED_UP->SWAPPING->STARTING_AGENT->HEALTH_CHECK->COMMITTED, ROLLBACK branch) with state persisted to updater_state.json (write-temp-rename) before each side-effecting step so a crash resumes/rolls back correctly. cmd/updater parses flags (check/apply), runs ON DEMAND (agent-triggered or scheduled-task), runs the FSM to completion, and exits — producing beyz-backup-updater.exe. It is NOT registered as a persistent Windows service in Sprint 1 (§0.6); the persistent watchdog is deferred to Sprint 8. Covers PRD 11 and the UPD-3/UPD-4/ARCH-8 crash-safe FSM.

**Done when:** beyz-backup-updater.exe builds; FSM persists before each step; killing the updater mid-FSM and re-invoking resumes/rolls back from updater_state.json; the binary runs on demand and exits (no persistent service registered).

### S1-T28 — Prism/Schemathesis mock SaaS + static update manifest fixtures for CI
`tooling · P0 · complexity M · depends on: S1-T02, S1-T21`

Stand up the contract-test harness: api/prism/ config + example responses driving a Prism mock from openapi.yaml (enroll issues a single-use token, returns 409 on replay; heartbeat returns cadence; tasks returns []), and a static signed test-manifest endpoint + Ed25519/BLAKE3 test vectors under test/fixtures for the updater. Add a Schemathesis job that fuzzes the agent's request shapes against the spec. Covers PRD 8 (SaaS registration placeholder/mock) and the ARCH-1 contract-test foundation.

**Done when:** `task contract` boots Prism, agent enroll/heartbeat/poll pass against it, replay returns 409, and the static manifest fixture verifies with the embedded test key.

### S1-T29 — Inno Setup installer: dual-binary install, ACL-locked folders, single (agent) service register/start, token input
`installer · P0 · complexity L · depends on: S1-T19, S1-T27`

Author installer/beyz-backup.iss (+ installer/scripts helpers): install beyz-backup-agent.exe and beyz-backup-updater.exe to C:\Program Files\BeyzBackup; create C:\ProgramData\BeyzBackup\{state,logs,update} and config.yaml with icacls /inheritance:r locking state\+update\ to SYSTEM+Administrators only (Users removed) and logs/config Users:R (SEC-6, PRD 15); register the SINGLE BeyzBackupAgent Windows service (LocalSystem, auto-start) and set sc.exe failure recovery actions; install beyz-backup-updater.exe as a binary but do NOT register it as a service (optionally create a disabled scheduled task for update checks) per §0.6; start the agent service after install (PRD 17). Accept the enrollment token via masked wizard page AND /TOKEN= silent param, written to a transient 0600 state\enroll.token (deleted on consume), excluded from /LOG (PRD 18, SEC-2). Support /VERYSILENT MSP deploy, upgrade/repair (preserve state), and uninstall (stop the agent service + remove any updater task, crypto-shred state by default with /KEEPSTATE opt-out, best-effort de-enroll). Covers PRD 13,14,15,16,17,18 and GAP-4.

**Done when:** Inno build produces BeyzBackupSetup-<ver>.exe; silent install creates ACL-locked folders (icacls shows Users removed from state\), registers+starts the single BeyzBackupAgent service, installs the updater binary WITHOUT registering a second service, enrolls from /TOKEN=; token absent from /LOG; uninstall stops the agent service and purges state.

### S1-T31 — CI pipeline: vet/lint/gosec, unit+contract tests, OpenAPI drift, gitleaks, race, Inno build
`tooling · P0 · complexity M · depends on: S1-T28`

Author .github/workflows/ci.yml + .golangci.yml: matrix (windows-latest, ubuntu-latest; go 1.23) running go vet, golangci-lint incl. staticcheck+gosec, unit tests with -race, Schemathesis/Prism contract tests, OpenAPI->code regeneration drift check, and gitleaks/trufflehog as a REQUIRED gate (no merge on committed secrets). Windows-only jobs: build the Inno installer + run installer/service Pester checks. Make the manifest-signature negative tests and gitleaks required checks (enforce CLAUDE.md no-placeholder-security). Covers PRD 20 and the security CI gates.

**Done when:** CI green on a clean tree; gitleaks and signature negative tests block merge on failure; OpenAPI drift detected; installer builds on the Windows runner.

### S1-T32 — Security-controls integration + negative-test task and gating
`shared · P0 · complexity M · depends on: S1-T12, S1-T14, S1-T23, S1-T24, S1-T29, S1-T10`

Cross-cutting security-controls task that wires and verifies the controls end-to-end and adds the explicit negative test suite (theme G): TLS pin enforced (non-pinned cert -> refused), token replay rejected (409), secrets never logged (grep agent+installer logs empty), state\ ACLs deny Users read (icacls assertion), tampered/unknown/revoked manifest + binary-hash-mismatch + downgrade all rejected, DPAPI tampered-blob fails closed, cloned-state detectable (mock heuristic). Reconcile CLAUDE.md env-var rule to 'no secrets in source/config' in code comments/docs. Covers PRD 23 (security notes) as enforced controls, not prose.

**Done when:** All theme-G negative tests pass and are wired into CI as required checks; ACL/secret-leak/pin/downgrade assertions green.

### S1-T33 — Unit test suite for shared kernel + agent core packages
`shared · P0 · complexity M · depends on: S1-T07, S1-T09, S1-T11, S1-T12, S1-T21, S1-T23`

Author the unit-test coverage mandated by CLAUDE.md across config (parse/precedence/forward-compat/secret-absence), identity (GUID stability, CSR validity, no-PII), hashing test vectors, wireversion (headers/426), manifest JCS byte-stability, backoff/jitter (full+decorrelated, cap, server-override), DPAPI wrap/unwrap, log redaction, and audit hash-chain. Target >=85% on security-critical packages (verify, enroll, DPAPI, config precedence, redaction, hash-chain), >=70% overall. Covers PRD 22 (test instructions) at unit level + CLAUDE.md test mandate.

**Done when:** go test ./... green; coverage report shows >=85% on the listed security-critical packages and >=70% overall.

### S1-T34 — Integration tests: enroll->register->heartbeat->poll vs mock + updater state-machine end-to-end
`shared · P0 · complexity M · depends on: S1-T16, S1-T27, S1-T28`

Author test/contract (agent client <-> Prism mock: full enroll->register->heartbeat->poll happy path, token single-use 409, server-controlled jittered cadence honored, 426 floor stops loop) and test/integration (updater verify->stage->swap->health-gate->rollback against fixture manifests: one bad-signature variant that never swaps, one valid-but-unhealthy variant that rolls back, plus TOCTOU re-verify). Covers PRD 22 integration level and the D/B/C test themes.

**Done when:** Integration suite green: enroll/heartbeat/poll pass vs Prism; consumed-token 409 handled; bad-signature manifest never swaps; unhealthy update rolls back and emits update.rolled_back.

### S1-T17 — internal/agent/license: load + verify server-signed license blob against embedded public key (advisory)
`agent · P1 · complexity S · depends on: S1-T10, S1-T09`

Implement internal/agent/license: on every start, load the signed license blob from the state store and verify its signature against an embedded public key (LIC-5); freeze the license claim-set struct (seats, quota, tenant binding, not_after) but enforce advisory-only this sprint. Emit license.signature_invalid audit event on failure. Reserved fields populated null in Sprint 1. Covers the LIC-1/4/5 'frozen claim set + verify path with test key' decision.

**Done when:** Unit test: valid signed blob verifies against embedded test key; tampered blob fails and emits audit event; missing blob is tolerated (advisory).

### S1-T20 — build/linux systemd unit + cross-platform service/state path abstraction
`agent · P1 · complexity S · depends on: S1-T10, S1-T18`

Author build/linux/beyz-backup-agent.service systemd unit (Type=notify, Restart=on-failure) and add the Linux path/permission abstraction (/etc/beyz-backup config 0644, /var/lib/beyz-backup/state 0600 root-only, /var/log/beyz-backup) behind the same config/state interfaces so the agent cross-compiles cleanly. The updater is prepped as a systemd timer + oneshot unit (on-demand, mirroring the Windows model) — NOT a second long-running service unit (§0.6). Units lint-clean; not gated to run in CI. Covers PRD 4 (Linux systemd preparation).

**Done when:** systemd-analyze verify passes on the unit files; linux/amd64+arm64 cross-compile of the agent succeeds; state paths resolve to the Linux locations.

### S1-T30 — Windows version-info/Authenticode signing hooks + Taskfile build/cross/dist
`tooling · P1 · complexity S · depends on: S1-T19, S1-T27`

Add build/windows assets (versioninfo.json, app manifest, icon, signing notes) and flesh out the Taskfile build/cross/dist targets: ldflags-inject version, cross-compile matrix (windows/amd64, linux/amd64+arm64), embed version resource into both .exe, and a documented (gated) Authenticode signtool step for both binaries + the installer (EV cert; real keys never in CI). Produce dist artifacts. Covers PRD 20 (build commands) + the binary-signing security control.

**Done when:** `task dist` cross-compiles all targets and emits versioned binaries with embedded version info; signtool step documented and invocable with a test cert; real keys absent.

### S1-T35 — Windows service-lifecycle + installer Pester tests
`installer · P1 · complexity M · depends on: S1-T29, S1-T19, S1-T27`

Author Pester/sc.exe tests: install->start->stop->restart->uninstall asserting the BeyzBackupAgent service state at each step, LocalSystem account, auto-start + recovery actions configured, crash-restart (kill process, SCM restarts, state survives); installer tests: silent install with /TOKEN creates folders, icacls shows Users removed from state\, the single BeyzBackupAgent service registered+started (updater installed as a binary, NOT a service), agent enrolls vs mock, token absent from /LOG, uninstall stops the agent service and purges state. Covers PRD 22 (Windows test instructions) and validates PRD 3,14,15,16,17,18 at runtime.

**Done when:** Pester suite green on a Windows runner: lifecycle transitions assert RUNNING/STOPPED correctly; ACL+no-token-in-log assertions pass; uninstall leaves no key/cert/license behind.

### S1-T36 — Six design records (DR-01..06) + docs: README, INSTALL, ARCHITECTURE/SECURITY/ROADMAP updates
`docs · P1 · complexity M · depends on: S1-T32, S1-T29, S1-T27, S1-T14`

Write the 6 one-page design records under docs/design/: DR-01 key-management, DR-02 enrollment-and-identity, DR-03 update-trust-and-rotation (incl. private-key custody), DR-04 tenancy-and-isolation, DR-05 protocol-versioning, DR-06 audit-event-schema. Author README.md (build/run/test entry, PRD 20/21/22), docs/INSTALL.md, and back-edit ARCHITECTURE.md (expanded component map, Redis presence/Postgres-transition split, tenant isolation, protocol versioning+compat window), SECURITY.md (reconciled secret-storage rule, token lifecycle, SPKI pin rotation, DPAPI/ACL model, audit hash-chain), CLAUDE.md (clarify env-var rule to 'no secrets in source/config'), and flip ROADMAP Sprint 1 status. Covers PRD 21,22,23,24 documentation + the 6-record G10 deliverable.

**Done when:** All 6 DR files present and one page each; README build/run/test instructions are accurate against the Taskfile; ARCHITECTURE/SECURITY/CLAUDE/ROADMAP back-edits committed.

### S1-T06 — pkg/storage StorageBackend interface + Capabilities + local no-op stub
`shared · P2 · complexity XS · depends on: S1-T01`

Define pkg/storage StorageBackend interface (Put/Get/Delete/List/Stat) and a Capabilities() flag set, plus a local no-op stub implementation only (STO-2). No real SMB/SFTP/S3/MinIO backends this sprint — just the frozen interface so Sprint 7 needs no break. Covers the reserved storage-interface decision.

**Done when:** Interface compiles; no-op stub satisfies it and returns NotImplemented/empty consistently; capability flags documented.
