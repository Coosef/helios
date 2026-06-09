# Beyz Backup — Sprint 1 Architecture Package

**Author:** Lead Software Architect (pre-implementation review)
**Date:** 2026-06-08
**Status:** For sign-off **before** Sprint 1 coding begins
**Scope of Sprint 1:** Agent Foundation only — Windows Service, config, structured logging, enrollment, heartbeat, task-poll placeholder, updater, Inno Setup installer. **No backup / restore / compression / encryption engine** (those are Sprints 3–6).

This package validates the architecture, surfaces the risks, and prepares Sprint 1 for successful implementation. **No source code is produced** and **no implementation is started** — by design.

---

## Documents in this package

| # | Document | What it contains |
|---|----------|------------------|
| 01 | [`01-RISK-REGISTER.md`](01-RISK-REGISTER.md) | **85 risks** across 9 dimensions (architecture, security, scalability, backup-engine, restore-engine, agent-update, licensing, storage, missing-requirements) + 6 added by an adversarial completeness review. Severity, likelihood, impact, recommendation, and Sprint-1 relevance for each. |
| 02 | [`02-TECHNICAL-DESIGN.md`](02-TECHNICAL-DESIGN.md) | The Sprint 1 design. **§0 = my binding decisions** (authoritative; resolves the contradictions). §1–§5 = the detailed design: goals/scope/deliverables/components/folder-structure/dependencies; enrollment/heartbeat/task-poll flows + API contract; database/config/logging; updater + installer; security controls + test strategy. |
| 03 | [`03-TASK-BREAKDOWN.md`](03-TASK-BREAKDOWN.md) | **36 implementation tasks** — ID, description, priority, dependencies, complexity, "done-when", plus a scope cut-line. |
| 04 | [`04-ACCEPTANCE-CRITERIA.md`](04-ACCEPTANCE-CRITERIA.md) | **42 measurable acceptance criteria** (38 must-have / 4 stretch), each with an explicit verification method, plus a Definition of Done. |
| 05 | [`05-OPEN-QUESTIONS.md`](05-OPEN-QUESTIONS.md) | **25 open questions** (21 blocking after §0.6 closed OQ-16 on updater scope; a signal in itself: the docs leave most foundational contracts unspecified) with options and an architect recommendation each. |

---

## Headline verdict

**The product vision is sound and the technology choices (Go agent, FastAPI/PostgreSQL/Redis, ZSTD/AES-256-GCM/BLAKE3, Inno Setup) are reasonable.** The problem is *altitude*: the docs are written at a level of abstraction that hides several **expensive-to-retrofit decisions that Sprint 1 silently freezes.**

The most important reframing for the team:

> **Sprint 1's real job is not "write an agent." It is "freeze the agent↔server contract, the on-disk format, the identity scheme, and the update trust root *correctly*."** Auto-update guarantees a long-lived, mixed-version fleet on customer machines that can **never be flag-day-migrated.** Whatever the first installer bakes in, you live with for years. And the agent is being built (Sprint 1) against a SaaS API that does not exist until Sprint 2 (ARCH-1) — so the contract must be authored as a committed, versioned artifact, not invented inside the Go code.

Nearly every **Critical** risk is one of three *decide-now / impossible-later* root decisions seen from different angles:

1. **Encryption-key custody & recoverability** (ARCH-5 / SEC-1 / BKP-8 / RST-1 / GAP-1)
2. **Update-signing trust root & key custody** (ARCH-4 / SEC-3 / UPD-1 / GAP-3)
3. **Manifest integrity & format** (BKP-3 / RST-2)

Resolve those three and the Critical list largely clears.

---

## The decisions that MUST be made before coding (blocking)

1. **Encryption-key recoverability model** — *zero-knowledge purity* vs *recoverability for non-technical SMB/hotel customers who will lose passphrases.* This shapes the **enrollment flow now** (it must branch on a `recovery_policy` field). Reserving null fields is **not** enough. → Needs **product/business sign-off.** Architect recommendation: **escrowed-default with opt-in zero-knowledge** (Technical Design §0.3).
2. **Update-signing** — Ed25519 + embedded public-key **set** + `key_id` + revocation-list field, with private-key custody offline/HSM (never in repo/CI). Unrotatable if shipped as a single key. → **Security sign-off** (§0.6, DR-03).
3. **Sprint-1 auth credential** = `agent_session_token` (24h, refreshed via heartbeat) **+ cert renewal via the `register` endpoint** — otherwise the 30-day enrollment cert kills the fleet on day 30 (§0.2).
4. **Identity** = server-issued opaque `device_id` + immutable `tenant_id`/`parent_org_id`/`region` bound into the cert; hardware signals advisory-only (§0.8).
5. **Updater scope cut** — ship *real* verification + agent-binary swap/rollback; **defer** the watchdog service, two-stage self-update bootstrap, and long-poll to Sprint 8 (§0.6). This is what keeps Sprint 1 to one sprint while honoring CLAUDE.md's "no placeholder security."

Full list with recommendations in [`05-OPEN-QUESTIONS.md`](05-OPEN-QUESTIONS.md).

---

## Reconciliations I made (the working design contradicted itself)

The detailed design was produced by parallel tracks that disagreed. As architect I issued binding rulings (Technical Design §0.1):

| Concern | Binding decision |
|---|---|
| Logging | `rs/zerolog` (behind a `Logger` interface) |
| Config | `knadh/koanf` (not Viper) |
| BLAKE3 | `zeebo/blake3` (not lukechampine) |
| Endpoints | 6 in the contract; 4 on the Sprint-1 happy path |
| Go version | 1.23.x floor, CI on 1.23 only (no split matrix) |
| Agent state | bbolt `agent-state.db` with DPAPI-wrapped values (key+cert inside, not loose files) |
| Updater state | standalone `updater_state.json` (separate process ≠ shared bbolt handle) |
| DPAPI | corrected: protects offline-disk-theft only; **the ACL is the live boundary**, not DPAPI |
| SPKI pin | bootstrap pin compiled into the binary; rotation pins in the locked state store, not `config.yaml` |

---

## Scope warning

As *fully elaborated*, the design is realistically **2–3 sprints** of work — the inflation is concentrated in the updater (a 2nd watchdog service + self-update bootstrap + full health-gated FSM, which is precisely the part the PRD wanted *stubbed*). Adopt the **§0.6 updater cut** and the **must-have/stretch cut-line** in [`03-TASK-BREAKDOWN.md`](03-TASK-BREAKDOWN.md) to fit one sprint. **Security stays enforcing** (Ed25519 verify, anti-rollback, ACL lockdown, secret redaction, TLS pinning are never cut); only the watchdog/self-update/long-poll/hot-reload niceties are deferred.

---

## Recommended back-edits to existing docs (do these alongside Sprint 1)

- **`docs/SECURITY.md`** — add: the reconciled secret-storage rule (DPAPI ≠ ACL), enrollment-token lifecycle (format/TTL/transport/replay), the fingerprint/identity scheme, SPKI-pin rotation, and the frozen audit-event schema.
- **`docs/ARCHITECTURE.md`** — expand the 5-component model to name the missing owners (Job Queue/Redis, Metadata/Manifest DB, Key Management, Enrollment CA, Update-Signing service, Licensing/Billing); add protocol versioning + the agent-compatibility window; document tenant isolation (shared schema + `tenant_id` leading column + RLS).
- **`CLAUDE.md`** — clarify "always use environment variables for sensitive configuration" → **"no secrets in source code or plaintext config"** (a `LocalSystem` service has no usable per-user env; the DPAPI-protected, ACL-locked state store is the resting place for machine secrets).
- **`docs/ROADMAP.md`** — flip Sprint 1 status once these are signed off.

---

## How this package was produced

A multi-agent analysis workflow ran 9 parallel risk analysts (one per dimension), 5 parallel design tracks, and a synthesis pass (task breakdown, acceptance criteria, open questions) followed by an adversarial **completeness critic**. The critic's findings — internal contradictions and 6 missing risk classes — were folded back in by the Lead Architect as the binding §0 decisions and the REV-* risks, rather than passed through. Net: **85 risks, a reconciled technical design, 36 tasks, 42 acceptance criteria, 25 open questions.**
