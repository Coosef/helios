# DR-04 — Tenancy & Isolation

- **Status:** Accepted — binding frozen in Sprint 1; server-side enforcement in Sprint 2
- **Date:** 2026-06-15
- **Related ADRs:** [ADR-003 — Agent Identity Model](../ADR/ADR-003-Agent-Identity-Model.md), [ADR-006 — Location/Site Scoping & RBAC Boundary](../ADR/ADR-006-Location-Site-Scoping-and-RBAC-Boundary.md)
- **Implementation:** `internal/agent/enroll`, `internal/agent/state` (binding produced & persisted)
- **Captures the Sprint-1 decisions for:** the multi-tenant binding model and the location/site boundary.

## Decision

"Multi-tenant first" is encoded **into the identity itself** at enrollment so isolation cannot be
retrofitted later. The **hard isolation boundary** is `tenant_id` (with `parent_org_id` for MSP
hierarchies and `region` for residency); all three are **server-authoritative and bound into the
agent certificate**. `location_id` is a **soft, mutable** intra-tenant scope that is **never
certificate-bound**. Sprint 1 **produces and persists the binding**; the server enforces it in
Sprint 2 — no agent-side RBAC exists.

## As-built (Sprint 1)

- **Certificate-bound (immutable):** `tenant_id`, `parent_org_id`, `region`. Changing any of these
  requires re-enrollment. They are the storage-key prefix and per-tenant dedup boundary for later
  sprints.
- **Advisory-in / authoritative-out:** the enrollment request carries `tenant_id`/`region`/
  `location_id` only as **hints**; the server validates and may override them, and its response is
  what the agent persists. The agent can never self-assign into a tenant/site it was not granted.
- **`location_id` is deliberately *not* a second isolation boundary** (ADR-006 calls binding it a
  "category error"): it is admin-reassignable, so binding it into an immutable cert would turn every
  device relocation into a re-enrollment. A genuine hard boundary is modeled as a separate **tenant**,
  not a location.
- **Persistence:** all four identifiers are **non-secret** and stored plaintext in the ACL-locked
  state store (`Put`, bucket `identity`); secrets (key, session token, license) are separately
  wrapped. The key-classification guard rejects a `PutSecret` of a non-secret key.

## Server-side model (frozen now, enforced Sprint 2)

- **Shared-schema isolation** with `tenant_id` as the **leading column** of every composite
  key/index plus **row-level security**; a documented sharding escape hatch exists for large/noisy
  tenants.
- **RBAC, role/user scoping, and location-filtered dashboards** are entirely server-side (Sprint 2+).
  The agent's only tenancy responsibility is to produce and persist the device → tenant (→ location)
  binding.

## References

- [ADR-003](../ADR/ADR-003-Agent-Identity-Model.md), [ADR-006](../ADR/ADR-006-Location-Site-Scoping-and-RBAC-Boundary.md)
- Code: `internal/agent/enroll/enroll.go`, `internal/agent/state/state.go`, `api/openapi.yaml`
- Related records: [DR-02](DR-02-enrollment-and-identity.md), [DR-06](DR-06-audit-event-schema.md)
