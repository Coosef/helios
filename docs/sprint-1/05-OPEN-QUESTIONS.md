# Beyz Backup — Sprint 1 Open Questions

> Decisions the docs leave open or contradictory that must be settled before coding (or that are expensive to retrofit). Each carries an architect recommendation; **BLOCKING** ones gate the start of Sprint 1.

**27 questions** — 21 blocking, 5 non-blocking, **1 resolved earlier** (OQ-16, closed by §0.6: updater scope). **OQ-26 / OQ-27 added and resolved by ADR-005** (T12 transport approval conditions; deferred/forward items tracked). The blocking set maps to the Critical risks; resolving them unblocks the bulk of the design.

## Blocking — decide before Sprint 1 coding

### OQ-01 — Agent/Server Contract · **BLOCKING**

**Q:** Will a versioned OpenAPI 3 spec for the four agent-called endpoints (POST /v1/enroll, POST /v1/agents/{id}/register, POST /v1/agents/{id}/heartbeat, GET /v1/agents/{id}/tasks) be authored and committed as the source of truth BEFORE the Sprint 1 Go HTTP client is written, with a mock/contract-test server (Prism/Schemathesis) stubbing it?

**Why it matters now:** Sprint 1 builds the agent against a SaaS API that does not exist until Sprint 2 (ROADMAP). The wire protocol, auth model, and request/response shapes get frozen by the installer rollout to customer machines. Without a committed contract artifact, the Go code becomes the de-facto spec and Sprint 2 must reverse-engineer it. Retrofitting a wire contract after agents are deployed in the field is extremely expensive (ARCH-1).

**Options:**
- A: Author OpenAPI 3 spec first, generate/validate the agent client against it, build a Prism mock server in Sprint 1.
- B: Hand-write Go structs now, write the OpenAPI spec retroactively in Sprint 2.
- C: Skip the formal spec; define request/response shapes inline in Go and document later.

**Architect recommendation:** A. Write the OpenAPI 3 spec as the committed source of truth and stand up a Prism/Schemathesis mock so the Sprint 1 placeholder validates against the exact artifact Sprint 2 implements. This makes the asserted 'API-first' principle a real artifact rather than a claim and de-risks the entire agent foundation.

### OQ-02 — Protocol Versioning · **BLOCKING**

**Q:** What is the agent<->server protocol/API versioning scheme, and where does the version live? Specifically: /v1/ in every path, X-Agent-Version + X-Protocol-Version headers on every request, a forward-compatible 'ignore-unknown-fields' message envelope, and a documented server compatibility window (e.g. supports agents N minor versions back, returns 426 Upgrade Required below the floor)?

**Why it matters now:** Auto-update guarantees a fleet of mixed-version agents permanently. With no version negotiation baked into the first installer, the server can never enforce later security upgrades (mTLS, signed-update enforcement floor) or reject incompatible agents gracefully — they will retry-loop instead. This is a one-line-now / impossible-later decision because the version fields must be present in the very first protocol the installer ships (ARCH-2, SEC-8, SCALE-9, BKP-7, GAP-5).

**Options:**
- A: /v1/ path prefix + version headers + min_supported_version floor + ignore-unknown-fields envelope, all from the first commit; server returns 426 below floor.
- B: Path version only (/v1/), add headers and floor logic later.
- C: No explicit versioning in Sprint 1; rely on additive-only changes.

**Architect recommendation:** A. Put /v1/ in every path, send agent + protocol version headers on every request, wrap every message in a forward-compatible envelope that ignores unknown fields, and document the compatibility window now. This is nearly free in Sprint 1 and unblocks every future security and format enforcement.

### OQ-03 — Update Signing · **BLOCKING**

**Q:** What signature algorithm, trust anchor, and signing-key custody model will the Sprint 1 updater use? Specifically: Ed25519 vs RSA-PSS/RSA-4096; public key (or pinned key-SET for rotation) compiled as a build-time constant into the updater binary; private key in offline HSM / cloud KMS that only the build pipeline can invoke (never on CI runners, laptops, or in the repo); and is signing a multi-person logged ceremony?

**Why it matters now:** The updater runs code as SYSTEM/root on every unattended endpoint — the highest-value supply-chain target in the system. The embedded public key becomes an immutable trust anchor the moment the first installer ships; the signing-key custody decision is irreversible without re-issuing every agent. ROADMAP defers 'Signed Updates' to Sprint 8, but the trust anchor and verification code path must be correct and present in the Sprint 1 binary. CLAUDE.md explicitly forbids placeholder security implementations and removing signature validation (UPD-1, SEC-3, ARCH-4, GAP-3).

**Options:**
- A: Ed25519, public key compiled into the updater, private key in offline HSM/cloud KMS invoked only by the build pipeline, manifest carries a key-id for rotation, signing is a logged multi-person action.
- B: RSA-4096/RSA-PSS with the same custody model.
- C: Defer real signing to Sprint 8; ship a no-op verify stub in Sprint 1.

**Architect recommendation:** A. Use Ed25519 (small, fast, no padding pitfalls), embed the public key at build time, keep the private key in an HSM/KMS, and include a key-id in the manifest. Reject option C outright — a no-op stub violates CLAUDE.md and leaves an unverified SYSTEM-level update path in the field.

### OQ-04 — Update Signing · **BLOCKING**

**Q:** Is the Sprint 1 updater's signature/hash verification REAL (verifies against an embedded test public key, hash read only from the signed manifest, anti-rollback enforced) or a placeholder that effectively returns true?

**Why it matters now:** PRD req 12 calls the updater 'placeholder logic' for signature verification and SHA256, but CLAUDE.md forbids placeholder security implementations in production code and forbids removing update signature validation. A no-op verify path that ships in the first installer is a permanent unverified-code-execution-as-SYSTEM vector; once a fleet trusts a no-op updater, you cannot retrofit enforcement without an out-of-band re-install. The verification order also matters: the binary hash must come ONLY from the signature-verified manifest, never a server header or filename (UPD-8, SEC-9, GAP-3, ARCH-4).

**Options:**
- A: Real verification from day one against an embedded test public key; verify manifest signature -> read expected hash from trusted manifest -> verify downloaded bytes -> anti-rollback check -> atomic replace. 'Placeholder' means orchestration/UI is stubbed, NOT crypto.
- B: Stub the verification (return true) in Sprint 1, implement real verification in Sprint 8.
- C: Verify hash only in Sprint 1, add signature verification in Sprint 8.

**Architect recommendation:** A. Interpret PRD 'placeholder' to mean the rollout orchestration is stubbed while the cryptographic verification path is fully real and exercised from the first build. This is mandated by CLAUDE.md and is the difference between a trustworthy updater and a brick-the-fleet liability.

### OQ-05 — Authentication / Credentials · **BLOCKING**

**Q:** What is the agent's control-plane credential in Sprint 1 given mTLS is deferred to Sprint 8: plain TLS + bearer token, or TLS + bearer token WITH SaaS-server certificate/SPKI pinning and a locally-generated keypair (CSR at enrollment) so the issued agent certificate can be promoted to true mTLS in Sprint 8 without changing the enrollment format?

**Why it matters now:** Without server-cert/SPKI pinning, enrollment and heartbeat are open to MITM (corporate TLS-intercepting proxies, rogue CAs) until Sprint 8. The credential format (token vs client cert) is set in Sprint 1 and frozen by the installer. If the agent doesn't generate a keypair and store a cert at enrollment now, promoting to mTLS later forces an enrollment-protocol break across the deployed fleet (SEC-4, SEC-5, GAP-5).

**Options:**
- A: TLS 1.2+/1.3 with SaaS SPKI pin + bearer token; agent generates keypair and submits a CSR at enrollment, stores the returned cert as a client credential ready to become mTLS in Sprint 8.
- B: Plain TLS + bearer token only, no pinning, defer all cert work to Sprint 8.
- C: Full mTLS now (pull Sprint 8 forward).

**Architect recommendation:** A. Pin the SaaS cert/public key, require TLS 1.2+, and treat the enrollment-issued agent cert as a forward-compatible client credential. This makes plain-TLS Sprint 1 acceptable and lets mTLS land in Sprint 8 as a switch-flip rather than a wire break. Full mTLS now (C) is unnecessary scope; no pinning (B) ships a MITM-exposed fleet.

### OQ-06 — Enrollment Token · **BLOCKING**

**Q:** What is the enrollment token's exact format, entropy, TTL, single-use enforcement mechanism, transport, and tenant scope? Specifically: >=128-bit CSPRNG opaque value; single-use enforced by atomic server-side row-state consumption (not client trust); short TTL (15-60 min vs 24h); bound to tenant_id (and parent_account_id for MSPs); delivered via stdin/secure-file/installer dialog rather than a logged CLI arg; who generates it (SaaS panel)?

**Why it matters now:** SECURITY.md says only 'single-use' with no issuance, expiry, revocation, transport, or replay/brute-force model. The registration call and the installer enrollment parameter (PRD req 18) are built NOW, so the token contract and its tenant binding are frozen in Sprint 1. A plaintext CLI arg leaks into installer/process logs; client-trusted single-use is forgeable; an untenanted token breaks multi-tenant isolation from the first agent (SEC-2, ARCH-7, GAP-2, LIC-6).

**Options:**
- A: >=128-bit CSPRNG opaque token, generated by the SaaS panel, tenant-scoped, atomic server-side single-use consumption, short TTL, server-side revocation, delivered via installer dialog/secure file/stdin (CLI arg zeroed and excluded from logs), rate-limited registration endpoint.
- B: Random token with 24h TTL, single-use, tenant-scoped, CLI-arg transport accepted.
- C: Minimal random token, single-use checked client-side, no TTL specified.

**Architect recommendation:** A. Define the full token lifecycle now: high-entropy, server-side atomic single-use, short TTL, tenant-bound, revocable, with non-logged transport and a rate-limited/lockable registration endpoint. The token is the root of enrollment trust and tenant isolation; it cannot be hardened retroactively.

### OQ-07 — Agent Identity / Fingerprint · **BLOCKING**

**Q:** What composes the agent fingerprint / device identity, and what is its stability policy? Specifically: a server-issued opaque device_id bound at enrollment (NOT a client-computed hardware hash) PLUS a persisted agent-generated random device GUID under C:\ProgramData\BeyzBackup (surviving app reinstall), with 2-3 hardware signals (MachineGUID + disk serial) reported only as clone-detection heuristics? And what is the documented VM-clone / re-image / hardware-change / device-replacement re-enrollment flow?

**Why it matters now:** Fingerprint stability is the binding point for device licensing, the agent cert, re-enrollment, and clone detection — all set in Sprint 1. A raw-hardware-hash fingerprint breaks license binding on VM clone, re-image, or hardware change; a config-file-only identity makes a cloned config.yaml indistinguishable from a legitimate device. The persisted GUID location and the re-enrollment/seat-reuse flow must be decided before enrollment is coded (ARCH-7, SEC-5, LIC-2, GAP-2).

**Options:**
- A: Server-issued opaque device_id (primary key) bound to the single-use token + agent-persisted random GUID in ProgramData + hardware signals as advisory clone-detection only; documented re-enrollment flow reuses the license seat.
- B: Client-computed stable-hardware hash as the primary fingerprint.
- C: Config-file device_id only, regenerated on reinstall.

**Architect recommendation:** A. Make the authoritative identity a server-issued opaque device_id embedded in the agent cert, backstopped by a persisted random GUID and advisory hardware signals for clone detection. Document the VM-clone and device-replacement re-enrollment flows in SECURITY.md before coding enrollment.

### OQ-08 — Key Management · **BLOCKING**

**Q:** What is the encryption key hierarchy and escrow/recovery MODEL (decide the model now, implement crypto in Sprint 4)? Specifically: per-device data key (DEK) generated on-device, wrapped by a tenant Key-Encryption-Key derived from a customer-held recovery passphrase via Argon2id; only the wrapped key stored/transmitted (never plaintext KEK/DEK to server); an explicit zero-knowledge-vs-recoverability decision and an optional opt-in escrow (customer recovery public key or Shamir split). Does config.yaml + the enrollment payload reserve forward-compatible fields (wrapped_device_key, key_id, key_wrap_version) now?

**Why it matters now:** CLAUDE.md/SECURITY.md stake the product on 'zero-knowledge' but ROADMAP defers Key Management to Sprint 4 — after the Sprint 1 config layout, enrollment exchange, and identity model that any key scheme must live inside are frozen, and after the first real backups (Sprint 3) exist in customer storage. Without an escrow decision before Sprint 3, recovery can NEVER be retrofitted onto already-encrypted historical data (a lost key = permanently unrestorable backups). The on-disk and enrollment formats need reserved key fields now to avoid a breaking change (SEC-1, ARCH-5, BKP-8, RST-1, GAP-1).

**Options:**
- A: On-device DEK wrapped by an Argon2id-derived customer KEK; server holds only the wrapped key (true zero-knowledge); optional opt-in escrow via customer recovery public key; reserve wrapped_device_key/key_id/key_wrap_version in config + enrollment now; force a 'recovery key confirmed' gate at enrollment.
- B: Provider-escrowed master key in SaaS HSM with break-glass (recoverable but NOT zero-knowledge).
- C: Defer the entire key model to Sprint 4, reserve no fields now.

**Architect recommendation:** A. Write a one-page key-management decision record now defining the per-device-DEK-wrapped-by-customer-KEK hierarchy, make escrow an explicit opt-in, reserve the key fields in the Sprint 1 config/enrollment formats, and document precisely what 'zero-knowledge' means (server holds a wrapped key it cannot unwrap). Reject C — it forces an agent rebuild and makes historical-data recovery impossible.

### OQ-09 — Config / Secret-at-Rest · **BLOCKING**

**Q:** What is the on-disk config and secret layout at C:\ProgramData\BeyzBackup, and how are secrets protected at rest? Specifically: split immutable operator-editable config.yaml (non-sensitive only) from a separate machine-protected state store holding the cert/private key, device GUID, and tokens; protect secrets with Windows DPAPI machine-scope (Linux root-only 0600 + future TPM); installer locks the folder ACL to SYSTEM + Administrators (remove Users) at create-time per PRD req 15; state writes are atomic (write-temp-rename). And how is CLAUDE.md's 'always use environment variables for sensitive configuration' reconciled with a headless Windows Service?

**Why it matters now:** config.yaml will hold enrollment/credential material, but no at-rest protection or ACL is specified — directly contradicting CLAUDE.md's 'never store secrets in source/config.' The installer (PRD req 15) creates the folder in Sprint 1, so ACL hardening and the secret-storage format are frozen now and must be purgeable by the uninstaller. The env-var rule is unworkable as literally stated for a SYSTEM service and needs reinterpretation to 'no secrets in source/config' (SEC-6, ARCH-8, STO-1, GAP-1).

**Options:**
- A: Split plaintext config.yaml from a DPAPI-protected state store; installer sets SYSTEM+Administrators-only ACL at folder creation; atomic write-temp-rename; reinterpret the env-var rule as 'no secrets in source/config, secrets encrypted at rest via OS keystore.'
- B: Single config.yaml with secret fields, folder ACL locked, no encryption-at-rest.
- C: All secrets in environment variables per the literal CLAUDE.md rule.

**Architect recommendation:** A. Separate secrets into a DPAPI-protected (Linux 0600) state store, keep config.yaml non-sensitive, have the installer lock ACLs at create-time, and make writes atomic. Update CLAUDE.md to clarify the env-var rule. Option C is infeasible for a headless service and would push secrets into a less protected surface.

### OQ-10 — Agent State Store · **BLOCKING**

**Q:** What is the local agent state store technology and structure: a flat file (YAML/JSON), or an embedded key-value/db (bbolt, SQLite)? This holds device GUID, cert/key handles, current installed version (for anti-rollback), last-known-good binary pointer, license blob, and crash-safe update state machine.

**Why it matters now:** Sprint 1 introduces durable agent state (registration result, version, update state) that must survive crashes and service restarts and support atomic writes. The updater's anti-rollback (persist current version), rollback (.bak pointer), and health-gate state machine all need crash-safe persistence chosen now; switching the state-store format after agents are deployed is a migration burden. Flat-file write-temp-rename is simplest but transactional multi-key updates favor bbolt (UPD-3, UPD-4, UPD-6, ARCH-8).

**Options:**
- A: Embedded bbolt (pure-Go, no cgo, transactional) for state; non-sensitive config stays in config.yaml.
- B: Flat JSON/YAML state file(s) with atomic write-temp-rename.
- C: SQLite (requires cgo or a pure-Go driver) for state.

**Architect recommendation:** B for Sprint 1 if state is small and writes are independent (atomic write-temp-rename is sufficient and dependency-free), but adopt A (bbolt) if the update state machine needs transactional multi-key consistency. Decide now and reserve a schema_version in the state file either way. Avoid SQLite/cgo unless a relational need emerges.

### OQ-11 — Windows Service Account · **BLOCKING**

**Q:** Which account does the Windows Service run as: LocalSystem, or a dedicated low-privilege service account (e.g. a virtual service account / gMSA)? This determines DPAPI scope for secrets, ACL targets on C:\ProgramData\BeyzBackup, and the privilege blast-radius of a compromised agent/updater.

**Why it matters now:** The agent runs continuously as a service and the on-demand updater runs as SYSTEM when invoked to replace the agent binary (§0.6: the updater is not a persistent service), so the service account is the privilege ceiling for the system's biggest supply-chain target. The account is set by the Sprint 1 installer (PRD req 16) and determines DPAPI machine/user scope and the exact ACL principals — both frozen at install time. LocalSystem is simplest and needed for backup read access to all files + service control, but maximizes blast radius (ARCH-8, SEC-6, STO-1).

**Options:**
- A: LocalSystem for the agent (needs broad file-read for backups + service control), with the updater performing the privileged binary swap; harden via restrictive ACLs.
- B: Dedicated virtual service account (NT SERVICE\BeyzBackup) with explicitly granted backup/restore privileges and file ACLs.
- C: gMSA (domain-managed) — enterprise only.

**Architect recommendation:** A (LocalSystem) for Sprint 1 pragmatism, because backup needs to read arbitrary files and the service must control itself, but document the dedicated-account hardening path (B) as a Sprint 8 goal and ensure DPAPI uses machine scope so a later account change does not strand encrypted secrets.

### OQ-12 — Task-Polling Transport · **BLOCKING**

**Q:** What is the Sprint 1 task-polling/heartbeat transport and cadence: fixed-interval short-poll, long-poll, or future push (WebSocket/SSE/gRPC)? And critically, is the interval SERVER-controlled (returned in the enroll/heartbeat response) with mandatory randomized jitter (±20%) and exponential-backoff-with-full-jitter on errors, plus mandatory HTTP keep-alive/connection reuse? What is the default interval?

**Why it matters now:** An unspecified, un-jittered, agent-hardcoded interval guarantees thundering-herd spikes at thousands-to-tens-of-thousands of agents and makes cadence un-tunable without shipping a new agent. The 'server tells agent its next poll delay' field, the jitter, and a 'work-available' / 'hold-for-N-seconds' hint must be in the wire protocol from Sprint 1 to allow a later cheap migration to long-poll/push without a wire break. Keep-alive must be mandated now so the future mTLS handshake amortizes across polls (SCALE-1, SCALE-3, GAP-5).

**Options:**
- A: Short-poll in Sprint 1 BUT server-returned next-poll-delay + ±20% jitter + exp-backoff-with-full-jitter on errors + mandatory keep-alive; protocol reserves a 'work-available' flag and 'hold-for-N-seconds' hint to enable long-poll/push later without a break. Default interval ~30-60s heartbeat.
- B: Fixed agent-hardcoded interval (e.g. 60s), no jitter, add server control later.
- C: Implement long-poll now.

**Architect recommendation:** A. Keep Sprint 1 implementation as simple short-poll, but make the cadence server-driven with jitter and backoff and reserve the work-available/hold hints in the protocol. This costs little now and prevents both a thundering herd and a future wire-format break. Option B's hardcoded interval is the single most urgent scaling hazard to avoid.

### OQ-13 — Heartbeat / Presence · **BLOCKING**

**Q:** Is the heartbeat write a PostgreSQL row write on the hot path, or does presence live in Redis (key-per-agent with TTL or a last-seen sorted set) with only online<->offline state TRANSITIONS flushed to Postgres? And is the heartbeat payload defined now as minimal (fingerprint + version + status enum + reserved license_state/reported_usage_bytes/health fields) and explicitly separated from the heavier task-poll response?

**Why it matters now:** Heartbeat-as-DB-write causes severe PostgreSQL write amplification and table bloat at fleet scale. The Redis-presence-vs-Postgres decision and the heartbeat payload shape are protocol/schema decisions; the payload is frozen by the installer. Reserving license_state, reported_usage_bytes, health/status, and clock-skew fields now keeps licensing, quota, observability, and update-ack from forcing later protocol breaks (SCALE-2, LIC-1, LIC-3, UPD-4, GAP-9).

**Options:**
- A: Presence in Redis (TTL key / last-seen sorted set), flush only online<->offline transitions to Postgres; minimal heartbeat payload with reserved license/usage/health fields, separated from task-poll.
- B: Write every heartbeat to Postgres now, optimize later.
- C: Heartbeat to Postgres but batched/async.

**Architect recommendation:** A. Design presence to live in Redis with only transitions persisted, keep the heartbeat payload minimal but with reserved forward-compatible fields, and separate it from the task-poll response. Even though the SaaS side lands in Sprint 2, the payload contract is set now — get the field set right.

### OQ-14 — Licensing Substrate · **BLOCKING**

**Q:** Will a one-page licensing contract be produced before Sprint 1 code freezes the primitives licensing must bind to (fingerprint, token, cert, heartbeat, audit log)? It must pin: the signed license claim set (tenant_id, device_id, device_count_limit, storage_quota_bytes, plan, expiry, grace_period, signature); that enrollment returns a signed license blob the agent persists and verifies against an embedded public key; reserved heartbeat fields for license_state + reported_usage_bytes; the offline-validity/grace window (e.g. 14-30 days); over-quota policy (never block restores); and the quota unit (stored-ciphertext-bytes-after-dedup).

**Why it matters now:** Licensing is a core monetization pillar yet has NO sprint in the 10-sprint roadmap and zero detail in ARCHITECTURE. Sprint 1 freezes every primitive it must bind to, so the enforcement substrate is being set with no licensing requirements shaping it. Zero-knowledge encryption + compression + dedup structurally prevents the server from measuring plaintext size, so the quota unit must be decided before pricing assumptions and the chunk/manifest schema lock. A signed offline license blob is the only way air-gapped backups can be both enforced and not silently free; the license-signing keypair custody is a root-of-trust decision (LIC-1, LIC-3, LIC-4, LIC-5).

**Options:**
- A: Author the licensing contract now; enrollment returns a signed, time-bounded (14-30 day) offline license blob the agent verifies locally against an embedded key; reserve license_state + reported_usage_bytes in heartbeat; quota = stored-ciphertext-bytes-after-dedup; never block restores; pin license-signing key custody.
- B: Reserve generic license fields in the protocol but defer the full contract to a later sprint.
- C: Ignore licensing in Sprint 1 entirely.

**Architect recommendation:** A. Write the one-page contract and reserve the forward-compatible fields now, even though enforcement ships later. The substrate (fingerprint, cert, heartbeat, audit) is frozen this sprint; getting the reserved fields and the offline-license trust model right now is cheap and prevents an agent rebuild. Reject C — it guarantees an expensive retrofit of the monetization core.

### OQ-15 — Multi-Tenancy · **BLOCKING**

**Q:** What is the multi-tenant isolation model and MSP hierarchy, and how is tenancy encoded into the Sprint 1 identity scheme? Specifically: shared-schema with mandatory tenant_id as the leading column of composite PKs/indexes + row-level security/query-guard, vs per-tenant schema; an immutable tenant_id (and parent_org_id for MSPs) embedded in the agent certificate at enrollment and authorized server-side on every request; and tenant_id as a hard prefix in the future chunk storage-key format.

**Why it matters now:** 'Multi-tenant first' is asserted (CLAUDE.md) but no partitioning, isolation boundary, or MSP model is defined. The enrollment token, agent cert, and fingerprint set in Sprint 1 must encode tenancy or multi-tenant isolation cannot be enforced from the first agent. The tenant boundary also determines the dedup scope (per-tenant, required by zero-knowledge) and the chunk storage-key format — both expensive to retrofit (ARCH-6, SCALE-4, STO-4, LIC-6).

**Options:**
- A: Shared-schema with mandatory tenant_id leading every composite key/index + RLS/query-guard; tenant_id + parent_org_id immutably embedded in the agent cert at enrollment and authorized server-side per request; tenant_id as hard storage-key prefix; document the large-tenant sharding escape hatch.
- B: Shared-schema with tenant_id columns but no RLS, isolation enforced only in application code.
- C: Per-tenant schema/database isolation.

**Architect recommendation:** A. Choose shared-schema with tenant_id as a first-class leading key + RLS/query-guard, bind tenant (and MSP parent) into the agent cert at enrollment, and make tenant_id the storage isolation prefix. This is the cheapest scalable model and the binding must exist from the first agent. Document the sharding escape hatch for noisy/large tenants even if unbuilt.

### OQ-16 — Updater Self-Replace · RESOLVED (§0.6) — no longer blocking

**Q (decided):** What is the self-update mechanic for a running Windows Service with a locked .exe, and is the updater a persistent watchdog service or a one-shot process?

**Decision (§0.6 — authoritative):** Sprint 1 ships the **agent-binary** replace mechanic for real — temp-write → verify Ed25519 signature + SHA256/BLAKE3 hash → atomic `MoveFileEx` rename (never in-place overwrite) → health-gated rollback against an integrity-checked `.bak`, with the `current/new/.bak` layout fixed now. The updater is an **on-demand binary (Option B), NOT a persistent watchdog service, and NOT a second registered Windows service.** The **two-stage updater self-update bootstrap** and the **persistent watchdog** are **deferred to Sprint 8** (where "Signed Updates" lives); the agent service's Windows Service recovery actions are the Sprint-1 backstop, and the updater binary itself is updated via the installer until then. The durable on-disk layout and single-service model are frozen now, so nothing irreversible is deferred — only additive code.

**Why it mattered:** A locked running .exe cannot overwrite itself, and the `current/new/.bak` layout + service-registration shape are frozen by the first installer (PRD req 16). §0.6 fixes those now; the deferred pieces are additive and ship cleanly in Sprint 8 (UPD-3, UPD-9).

**Options:**
- A: Persistent watchdog **service** + two-stage self-update now. — **REJECTED** (PRD req 12 asked for placeholder logic; this is ~2–3 sprints of the highest-risk code).
- B: One-shot **on-demand** updater, no persistent watchdog, rely on the agent service's Windows recovery actions. — **CHOSEN (§0.6)**, with the real verify/swap/rollback of the agent binary kept fully enforcing.
- C: In-place overwrite, no atomicity. — REJECTED (bricks the agent on an interrupted swap).

**Resolution:** **Option B.** Security-critical replace/verify/rollback of the agent binary is fully real in Sprint 1; the persistent watchdog and updater self-update bootstrap are deferred to Sprint 8. No security control is weakened.

### OQ-17 — Updater Rollback / Health Gate · **BLOCKING**

**Q:** What is the post-update health gate, rollback trigger, and 'what gets restored' contract? Specifically: after restart the new agent must start AND send one successful heartbeat / 'update OK' self-report to the SaaS within a bounded timeout (e.g. 90s) before the update commits and .bak is deleted; on failure the updater auto-restores the previous binary AND the previous config snapshot from the .bak set and restarts; the bundle versions binary + config-schema together; anti-rollback uses the SIGNED manifest version, never the filename/server header.

**Why it matters now:** Rollback is a placeholder with no trigger, no health check, and no defined restore contract — yet it is the last defense against a fleet-wide brick. The heartbeat sender is already Sprint 1 scope, so wiring the heartbeat-ack into the update success signal is cheap to design now. What the bundle versions together (binary + config) and the anti-rollback comparison source are manifest-format and agent-state decisions frozen this sprint (UPD-4, UPD-6, UPD-3).

**Options:**
- A: Health gate = process start + successful heartbeat/'update OK' to SaaS within 90s; on failure auto-restore binary + config snapshot from .bak and restart; bundle versions binary+config together; anti-rollback compares the signed manifest's monotonic version against persisted current-version.
- B: Health gate = process starts and stays up for N seconds (no heartbeat ack); restore binary only.
- C: No automatic rollback; manual recovery via re-install.

**Architect recommendation:** A. Define the heartbeat-acked health gate, the binary+config rollback pair, and signed-manifest-based anti-rollback now. Reusing the Sprint 1 heartbeat as the commit signal is nearly free and is the difference between an update that self-heals and one that requires touching every machine.

### OQ-18 — Update Rollout Control · **BLOCKING**

**Q:** Will the update-check/manifest protocol be server-DIRECTED with staged/canary rollout and a kill-switch from Sprint 1? Specifically: the SaaS update-check response carries target_version + a rollout cohort/percentage + an explicit per-device 'update_allowed' boolean (kill-switch), with server-side per-tenant version pinning — so the agent updates only when told, never autonomously pulling 'latest'.

**Why it matters now:** With no staged rollout or kill-switch, a bad signed update reaches 100% of the fleet instantly with no abort. The rollout-cohort, target_version, and update_allowed fields are manifest/version-check protocol fields that must exist in the very first format the installer ships; adding them after deployment is a wire-format break. Designing the protocol as server-directed now (rather than agent-pulls-latest) is the only way to gain a kill-switch and canary capability later without re-issuing agents (UPD-5, SCALE-9, ARCH-2).

**Options:**
- A: Server-directed updates from Sprint 1: response carries target_version + rollout cohort/% + per-device update_allowed kill-switch + per-tenant pin; agent updates only when instructed; rollout orchestration UI ships later but the fields exist now.
- B: Agent pulls 'latest' from a manifest; add rollout control fields later.
- C: Reserve only target_version now; add cohort/kill-switch in Sprint 8.

**Architect recommendation:** A. Make the update protocol server-directed and reserve the rollout cohort and per-device kill-switch fields in the manifest/check response now. The orchestration can be stubbed, but the wire fields and the 'server decides, agent obeys' model must be present in the first installer to make a future canary/kill-switch possible.

### OQ-19 — Update Manifest Format · **BLOCKING**

**Q:** What is the canonical update-manifest format and signed scope? Specifically: a versioned JSON document with explicit schema_version; reserved fields for key_id, monotonic version + min_acceptable_version floor, rollout cohort, kill_switch, a (possibly empty) key-revocation list, and a per-platform artifacts array each carrying the binary's BLAKE3/SHA256; the Ed25519 signature covers the ENTIRE canonicalized manifest (not just the binary hash); transport over HTTPS pinned to the SaaS; manifest treated as immutable/cache-busted.

**Why it matters now:** The update-manifest wire contract is undefined; once the first installer ships, a non-versioned/non-extensible manifest cannot carry key rotation, anti-rollback floors, rollout control, or revocation without a break. The signature MUST cover the whole canonical manifest so the binary hash, version, and rollout flags are all trusted; if the hash comes from outside the signed manifest the SHA256 check is theatre. These are format decisions frozen by the first published manifest (UPD-7, UPD-2, UPD-8, SEC-9).

**Options:**
- A: Versioned canonical JSON with schema_version + reserved key_id/min_version/rollout/kill_switch/revocation-list + per-platform artifacts[hash]; Ed25519 over the whole canonicalized manifest; HTTPS pinned, immutable/cache-busted. Reserve unused fields now.
- B: Minimal JSON (version + binary URL + hash + signature over the binary only), extend later.
- C: Sign only the binary hash, keep the manifest unsigned.

**Architect recommendation:** A. Freeze a versioned, canonical, fully-signed manifest with all extension fields reserved (even if unused until Sprint 5/8). Signing the whole canonicalized manifest and reading the binary hash only from it closes the SHA256-theatre gap and makes rotation/rollout/anti-rollback possible without a future break.

### OQ-20 — Audit Log · **BLOCKING**

**Q:** What is the structured audit-event schema, and is it tamper-evident and mirrored to the SaaS from Sprint 1? Specifically: event_type, tenant_id, device_id, monotonic sequence, server-anchored timestamp, actor, outcome; per-record hash-chaining (each entry includes the prior entry's hash) for local tamper-evidence; near-real-time shipping of security events to the SaaS so the authoritative copy is off the endpoint (the SYSTEM agent can rewrite its own logs); and a PII-redaction rule for paths/usernames.

**Why it matters now:** Audit log is specified contradictorily — a Sprint 1 security-event source (SECURITY.md) AND a Sprint 8 deliverable (ROADMAP) — with no integrity design, while the SYSTEM agent can rewrite its own logs. The event schema is expensive to change retroactively, so Sprint 1 must emit enrollment/update/auth/license events through the final schema even if SaaS-side aggregation lands in Sprint 8. Hash-chaining and mirroring satisfy the docs' own 'tamper detection' requirement (ARCH-9, SEC-7, GAP-7, LIC-5).

**Options:**
- A: Define the audit schema now (event_type, tenant_id, device_id, monotonic seq, server-anchored timestamp, actor, outcome); local hash-chaining for tamper-evidence; mirror security events to the SaaS in near-real-time; redact PII; Sprint 1 emits enrollment/update/auth/license events through it; Sprint 8 adds only central aggregation/transport.
- B: Define the schema now and emit events locally; defer hash-chaining and SaaS mirroring to Sprint 8.
- C: Emit ad-hoc structured logs now; design the audit schema in Sprint 8.

**Architect recommendation:** A. Lock the audit-event schema now and emit security events through it from day one, with local hash-chaining and near-real-time SaaS mirroring so a local wipe cannot erase the trail. Deferring only the central store (not the schema or the emit path) resolves the doc contradiction and avoids a breaking format change.

### OQ-22 — Mock SaaS Server · **BLOCKING**

**Q:** Will a mock/contract SaaS server be built or generated in Sprint 1 so the agent's enrollment, registration, heartbeat, and task-polling code can be exercised end-to-end against the committed OpenAPI spec before the real SaaS exists in Sprint 2?

**Why it matters now:** The agent cannot be meaningfully tested or demoed in Sprint 1 without a server to talk to, and CLAUDE.md requires tests for new functionality plus integration tests. A generated mock (Prism from the OpenAPI spec, or a thin FastAPI stub) lets the agent's HTTP paths, retry/backoff, and state machine be integration-tested against the exact contract Sprint 2 implements — tying directly to OQ-01.

**Options:**
- A: Generate a Prism mock from the OpenAPI spec (zero hand-written server, always in sync with the contract) for Sprint 1 integration tests.
- B: Hand-write a thin FastAPI mock server (matches the real Sprint 2 stack, can hold simple state for single-use-token/enrollment tests).
- C: No server; unit-test the agent's HTTP layer with httptest stubs only.

**Architect recommendation:** B for stateful flows that need real behavior (single-use token consumption, enrollment issuing a cert/license blob), backed by A (Prism) for pure contract conformance. Build at least a minimal stateful FastAPI stub in Sprint 1 so enrollment/heartbeat are integration-tested end-to-end. Pure httptest stubs (C) won't validate the cross-cutting flows.

### OQ-25 — NFRs / Platform Support / Signing Certs · **BLOCKING**

**Q:** What are the supported OS versions (which Windows: 10/11/Server 2016-2025; which Linux distros/systemd versions), the RPO/RTO/backup-success-SLA targets per tier, and is an Authenticode code-signing certificate (EV vs OV) procured so the Sprint 1 agent/updater/installer binaries are OS-trusted? Also: device decommission / crypto-shred / uninstall offboarding flow, and proxy/IPv6/egress-FQDN support in the HTTP client.

**Why it matters now:** No NFRs (RPO/RTO/SLA), supported-OS matrix, or signing-cert plan appear anywhere. Authenticode signing is procurement-lead-time-bound and the installer + both .exes are produced in Sprint 1 — unsigned binaries trigger SmartScreen/UAC friction and undermine the trusted-updater story. The uninstaller (PRD installer scope) must be able to fully purge secrets/keys/cert (crypto-shred for GDPR), which constrains where Sprint 1 writes them. Proxy/IPv6/egress support shapes the Sprint 1 HTTP client. The OS matrix bounds DPAPI/service/VSS assumptions (GAP-4, GAP-6, GAP-8, and the prompt's explicit Authenticode/OS-version probes).

**Options:**
- A: Pin the supported-OS matrix, set explicit RPO/RTO/SLA per tier, procure an Authenticode (EV preferred for instant SmartScreen reputation) cert now, define the uninstall/crypto-shred offboarding flow and write secrets where the uninstaller can purge them, add proxy/IPv6/egress-FQDN support to the Sprint 1 HTTP client.
- B: Pick a minimal OS matrix + procure a signing cert now; defer RPO/RTO/SLA and proxy/offboarding to later sprints.
- C: Leave OS support broad/unspecified, sign binaries later, defer NFRs and offboarding.

**Architect recommendation:** A for the procurement-bound and Sprint-1-frozen items (Authenticode cert — start procurement immediately given lead time; supported-OS matrix; uninstall/crypto-shred secret-purge path; proxy/IPv6/egress in the HTTP client). RPO/RTO/SLA targets are NOT strictly Sprint-1-blocking and can be a fast-follow doc, but the OS matrix, signing cert, offboarding, and network-reality support do affect Sprint 1 deliverables and should be settled now.

## Non-blocking — decide during / before the relevant sprint

### OQ-21 — Manifest / Chunk / Hash Format

**Q:** Will the backup manifest, chunk-identity, chunking, and hashing formats be DESIGNED (not implemented) in Sprint 1? Specifically: a versioned manifest (manifest_schema_version, device fingerprint, timestamp, parent restore-point id, chunker params, hash_algo id, crypto_algo id, ordered file->chunk list with plaintext-hash + ciphertext storage-key + size + compression flag, self-hash + signature); content-defined chunking (FastCDC) with recorded min/avg/max params; ONE default hash (BLAKE3) with algorithm-tagged digests ('blake3:<hex>'); per-tenant dedup scope with storage keys as HMAC(device_key, plaintext_hash); chunk storage-key format {tenant_id}/v1/blake3/{hash}.

**Why it matters now:** The manifest is the single point of failure for every restore, yet its format, the chunk-ID scheme, the chunking strategy, and the BLAKE3-vs-SHA256 choice are all undecided. These are foundational to enrollment-derived keying, the config schema, and the on-disk/on-wire formats; encrypt-after-compress destroys cross-file dedup and the only fix (convergent encryption) breaks zero-knowledge, so the per-tenant dedup boundary must be decided now. The hash-algorithm-id and format-version fields must be present in the very first format Sprint 3 emits or historical backups become unverifiable/unrestorable across versions (BKP-1/2/3/5, RST-2/3/9, STO-4/5, SCALE-6).

**Options:**
- A: Design and freeze the versioned manifest + FastCDC params + BLAKE3 default with algorithm-tagged digests + per-tenant dedup with HMAC(device_key, plaintext_hash) storage keys + {tenant_id}/v1/blake3/{hash} key format, as a Sprint 1 design artifact; implementation lands Sprints 3-5.
- B: Design only the manifest schema_version + hash_algo id now; defer chunking/dedup/key-format decisions to Sprint 3-5.
- C: Defer all format decisions to the sprint that implements them.

**Architect recommendation:** A. Produce the manifest/chunk/hash/dedup design now (BLAKE3 as the single backup hash with algorithm tags, SHA256 reserved for the update path; FastCDC; per-tenant dedup via keyed chunk IDs). It is cheap as a design artifact and prohibitively expensive to retrofit once production backups exist in customer storage. Even though no backup code ships in Sprint 1, these decisions shape the config/enrollment keying and the reserved format-version fields.

### OQ-23 — Config Hot-Reload

**Q:** Does the agent support config.yaml hot-reload (watch + apply without restart), or is a service restart required for config changes in Sprint 1? If hot-reload, which fields are hot-reloadable (log level, poll interval, throttle) vs restart-only (endpoints, identity, secrets)?

**Why it matters now:** config hot-reload behavior shapes the config-loading architecture and the operator experience for a headless fleet, and it interacts with the immutable-config / mutable-state split (OQ-09). Server-controlled cadence (OQ-12) reduces the need to hot-reload the interval, but operators will expect log-level and throttle changes to apply without bouncing a backup mid-flight. Deciding the reloadable field set now avoids re-architecting the config layer later.

**Options:**
- A: Hot-reload a defined safe subset (log level, poll interval, bandwidth throttle, schedule window) via file-watch + atomic re-read; identity/endpoints/secrets are restart-only.
- B: Restart-only for all config in Sprint 1; add hot-reload later.
- C: Full hot-reload of everything.

**Architect recommendation:** A if it is cheap with the chosen config library, otherwise B for Sprint 1 with a documented reloadable-field set reserved for later. Prefer server-controlled values (OQ-12) for cadence so local hot-reload is mostly a convenience for log level and throttle. Never hot-reload identity or secrets. This does not block the core agent skeleton.

### OQ-24 — Storage Credential Model

**Q:** How are customer storage-target credentials (SMB/SFTP/S3/MinIO) custodied and delivered, and is a CredentialStore abstraction + config slot defined in Sprint 1 even though no target is wired until Sprint 7? Recommendation: store them in the SaaS DB encrypted with a per-tenant KMS/envelope key, deliver to the agent just-in-time over the authenticated channel (NOT persisted in config.yaml); if local persistence is unavoidable, use Windows DPAPI machine-scope. Treat storage credentials as a DISTINCT secret class from the file-encryption key.

**Why it matters now:** Storage credentials have no defined storage location, encryption, or custody model, yet the config schema, the secret-at-rest format, and the installer ACLs that must protect them are frozen in Sprint 1. Putting them in plaintext config.yaml contradicts CLAUDE.md; deciding just-in-time delivery vs local persistence now shapes the config schema slot and the CredentialStore interface that Sprint 7 will fill (STO-1, STO-2, GAP-1, SEC-6).

**Options:**
- A: Define a CredentialStore abstraction + a config schema slot now; custody in SaaS DB under a per-tenant envelope key; just-in-time delivery over the authenticated channel; DPAPI machine-scope only if local persistence is required; storage creds are a separate secret class from the file-encryption key.
- B: Reserve a config.yaml section for storage creds, encrypt at rest with DPAPI, persisted locally.
- C: Defer the storage credential model entirely to Sprint 7.

**Architect recommendation:** A. Define the CredentialStore abstraction and the config slot now and decide just-in-time delivery as the default, so the Sprint 1 config schema and secret-at-rest design account for this distinct secret class. The interface and the config slot are cheap now and expensive to retrofit once the config format ships.

### OQ-26 — Fleet Pin-Rollover Runbook & Runtime Pin Reload · **RESOLVED (ADR-005) — deferred work tracked**

**Q:** How does a Beyz-controlled SPKI pin rotation roll across the fleet without lockout, and does the agent need runtime pin reload?

**Why it matters now:** S1-T12 makes SPKI leaf-key pinning the control-channel trust anchor. The pin set is captured at client construction; a delivered rotation pin only takes effect on agent restart. Without a tested rollover process, a routine key rotation risks a fleet lockout (REV-2).

**Decision (ADR-005):** Runtime pin reload is **NOT required in Sprint 1** — accepted behavior is **pin rollover requires an agent restart** (pin set = compiled-in bootstrap ∪ ACL-locked state store, §0.5). A **dynamic pin provider / runtime reload is deferred to Sprint 8.**

**Forward requirement (pre-production):** a **fleet pin-rollover runbook must be written and tested before production release** — deliver overlapping pin over the trusted channel → confirm fleet adoption → rotate the key; installer-driven re-pin is the documented last resort. This is a **production release blocker** (operational pre-condition for a single controlled pilot).

### OQ-27 — Enterprise Network Compatibility (TLS Inspection / Authenticated Proxy) · **RESOLVED (ADR-005) — documented constraints**

**Q:** What enterprise network topologies can the agent control channel operate in, given SPKI pinning?

**Why it matters now:** SPKI pinning intentionally defeats TLS-intercepting proxies, and the stdlib transport cannot do NTLM/Kerberos proxy auth. These are deployment-blocking realities that must be stated before sales/onboarding commit.

**Decision (ADR-005), documented constraints:**
- **TLS inspection / SSL interception breaks SPKI pinning by design** — the agent control-channel FQDN MUST be **bypassed (allow-listed)** in the inspection proxy.
- **NTLM / Kerberos authenticated proxy is NOT supported in Sprint 1.**
- **Basic proxy** via the system environment proxy or an explicit proxy URL **IS supported.**

These constraints belong in `SECURITY.md` / `INSTALL.md` and the customer onboarding checklist.
