# DR-05 — Protocol Versioning & Compatibility

- **Status:** Accepted — implemented in Sprint 1 (Agent Foundation)
- **Date:** 2026-06-15
- **Related ADRs:** [ADR-004 — Protocol Versioning](../ADR/ADR-004-Protocol-Versioning.md)
- **Implementation:** `pkg/wireversion`, `api/openapi.yaml` (+ generated `pkg/proto`), `internal/transport/{httpclient,saasclient}`
- **Captures the Sprint-1 decisions for:** the control-plane contract, version negotiation, and forward-compatibility.

## Decision

The control plane is **contract-first**: `api/openapi.yaml` (OpenAPI 3.1) is the committed source
of truth, and the Go client (`pkg/proto`) is **generated** from it with a CI drift gate. Every
request carries an **agent version** and a **protocol version**; an incompatible agent is told to
upgrade with **`426 Upgrade Required`** (routed to the updater) rather than being allowed to retry
forever. The wire is **forward-compatible by construction** so later sprints add fields without
breaking deployed Sprint-1 agents.

## As-built (Sprint 1)

- **Headers on every request:** `X-Agent-Version` (injected at build via ldflags from the single
  repo-root `VERSION` file) and `X-Protocol-Version` (`CurrentProtocolVersion = 1`). The transport
  (`httpclient`) injects them via `wireversion.RequestEditor`; `saasclient` does not duplicate them.
- **Compatibility floor:** `MinSupportedProtocolVersion` is a distinct constant (equal to current in
  Sprint 1) so a future multi-generation server can negotiate. `426` is a terminal, non-retried
  signal that surfaces as `ErrUpgradeRequired` and exits the agent with a dedicated code, handing off
  to the updater.
- **Forward-compatible envelopes:** every request/response declares `additionalProperties: true` and
  carries integer `schema_version`s; unknown fields are ignored and reserved fields are present
  (nullable) from day one — this is the substrate ADR-001/002/003's reserved fields rely on. Task
  envelopes recognise reserved types (`config_refresh`, `update_check`) without executing them and
  skip unknown types without error.
- **Errors** use RFC 9457 `problem+json`; the `426` body carries `min_supported_version` /
  `min_supported_protocol` as extension members. The `426` body read is capped (1 MiB) to fail-closed
  against a hostile/oversized error body.
- **Generation & drift:** `task gen:proto` regenerates `pkg/proto` from the spec (3.1 → 3.0
  down-convert → `oapi-codegen`); `task check:generated` fails CI on any drift, so the spec — not the
  Go code — is authoritative. A Prism mock of the same spec backs `task contract`.

## Policy / deferred

- **Compatibility window = N − 3 minor versions** is a governance policy (documented, revisited per
  release), not a hardcoded constant.
- Server-side multi-generation negotiation and runtime version-gated routing land with the SaaS in
  **Sprint 2**; the client contract is frozen now.

## References

- [ADR-004](../ADR/ADR-004-Protocol-Versioning.md), `api/openapi.yaml`
- Code: `pkg/wireversion/*`, `internal/transport/{httpclient,saasclient}`, `internal/buildinfo`
- Related records: [DR-02](DR-02-enrollment-and-identity.md), [DR-03](DR-03-update-trust-and-rotation.md)
