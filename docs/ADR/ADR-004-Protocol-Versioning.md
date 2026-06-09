# ADR-004 — Protocol Versioning

- **Status:** Accepted
- **Date:** 2026-06-08
- **Deciders:** Chief Architect
- **Related:** OQ-01 / OQ-02, Technical Design §0.1 / §0.8 / §2, Risks ARCH-1 / ARCH-2 / SEC-8 / SCALE-9 / BKP-7 / GAP-5

## Context

Sprint 1 builds the agent against a SaaS API that does not exist until Sprint 2 (ARCH-1), and **auto-update guarantees a permanently mixed-version fleet** on customer machines that can never be flag-day-migrated. Whatever protocol the first installer ships is frozen in the field for years. Without version negotiation baked into that first protocol:

- the server can never **enforce** later security upgrades (mTLS, a signed-update floor) or reject incompatible agents gracefully — incompatible agents retry-loop instead, and a heartbeat that silently fails looks like an offline device on a *data-protection* product;
- the server can never safely evolve its schema.

The version fields and the forward-compatibility rules must be present in the **very first** wire contract. This is a one-line-now / impossible-later decision.

## Decision

1. **The OpenAPI 3.1 spec is the committed source of truth** (`api/openapi.yaml`), authored and committed **before** the Go HTTP client is written; the client is generated from it and a mock server (Prism + a thin stateful stub) validates the agent against the exact artifact Sprint 2 implements. The Go code is **not** the spec.

2. **Path version:** every endpoint is under **`/v1/...`** (`/v1/enroll`, `/v1/agents/{id}/register`, `/v1/agents/{id}/heartbeat`, `/v1/agents/{id}/tasks`, `/v1/agents/{id}/tasks/{id}/ack`, `/v1/agents/{id}/tasks/{id}/status`).

3. **Version headers on every request:** `X-Agent-Version` (agent semver, injected at build via ldflags) and `X-Protocol-Version` (integer). The server records and gates on both.

4. **Compatibility floor:** when an agent is below the minimum supported protocol/version, the server returns **`426 Upgrade Required`** with a `min_supported_version` body; the agent stops calling and routes to the updater rather than retry-looping.

5. **Compatibility window (policy):** the server supports agents up to **N = 3 minor versions** back; the window is documented in SECURITY.md / ARCHITECTURE.md and revisited per release.

6. **Forward-compatible envelopes:** every wire message and the update manifest carry a `schema_version` integer, and **both sides ignore unknown fields** (never error on extra keys). This lets Sprint 2–8 add fields (key material, licensing, rollout, health) without a wire break — the reserved-fields strategy across the ADRs depends on this rule.

7. **Error semantics:** uniform RFC 9457 `application/problem+json` bodies; mutating calls accept an `Idempotency-Key`.

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| **Path version only (`/v1/`), add headers + floor later** | Gives routing but no per-request negotiation or enforceable floor; the server cannot reject or upgrade-gate specific agent versions, and adding headers later means the earliest agents never send them. |
| **No explicit versioning; rely on additive-only changes** | "Additive only forever" is unenforceable across an auto-updating fleet and provides no way to *require* a security upgrade (e.g., signed-update enforcement, mTLS). One breaking need = a fleet flag day. |
| **Header-only versioning (no `/v1/` path)** | Loses unambiguous routing and human-readable URL versioning; mixing v1/v2 handlers on one path is error-prone for the Sprint-2 server. |
| **Let the Go structs be the contract, write the spec retroactively** | Makes the asserted "API-first" principle a fiction; Sprint 2 must reverse-engineer the wire shape from agent code already deployed in the field. Contract-first (committed OpenAPI) is the explicit ARCH-1 mitigation. |

## Consequences

**Positive**
- The fleet can evolve gracefully; the server can enforce minimum versions and route incompatible agents to update instead of retry-looping.
- Reserved-field + ignore-unknown-fields forward compatibility lets every later sprint (key mgmt, licensing, rollout, audit) extend the wire format with no break — this single rule underpins ADR-001/002/003's "reserve now" strategy.
- A committed OpenAPI spec gives Sprint 2 an exact build target and gives Sprint 1 a real contract-test artifact.

**Negative / costs**
- The server must honor a compatibility window (N minor versions back), implying multi-version handling discipline and deprecation management.
- Version hygiene (bump `X-Protocol-Version`/`schema_version` on every breaking field semantics change) is an ongoing engineering responsibility.
- A self-authored mock proves self-consistency, not Sprint-2 parity (REV-6): the OpenAPI spec must be **co-owned and reviewed** by whoever builds the Sprint-2 backend, and server-side contract tests added in Sprint 2.

**Sprint-1 impact**
- `api/openapi.yaml` (S1-T02) encodes `/v1/`, the headers, the 426 floor, the `schema_version` envelopes, and the reserved fields.
- `pkg/wireversion` (S1-T04) holds `AgentVersion`/`ProtocolVersion`/`MinSupportedProtocol`, sets the headers on every outbound request, and parses `426`.
- `internal/transport/saasclient` (S1-T16) is the only place that knows endpoint paths and maps `426`/problem+json.
