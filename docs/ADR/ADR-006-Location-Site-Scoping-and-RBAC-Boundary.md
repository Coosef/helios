# ADR-006 — Location/Site Scoping & RBAC Boundary

- **Status:** Accepted
- **Date:** 2026-06-09
- **Deciders:** Chief Architect
- **Related:** ADR-003 (Agent Identity Model), Technical Design §0.8 / §3, S1-T13 (`internal/transport/saasclient`), S1-T14 (enrollment workflow), S1-T10 (`internal/agent/state`), Risks SCALE-4 / REV-5 / LIC-6
- **Supersedes/extends:** ADR-003 decision 5 (tenancy binding) — adds the location/site axis and the human-user RBAC boundary.

## Context

The product must support three human-user scopes — **Helios Control Plane**, **Tenant Admin**, and **Location/Site-scoped users** — with hard rules: a user from one tenant must never see another tenant's data; a Tenant Admin may authorize users only for selected locations/sites; every dashboard, report, device, backup job, and restore view must auto-filter by the viewer's assigned tenant/location scope; the RBAC model must stay future-proof for an MSP/reseller hierarchy; and some tenant data may later be separated at the DB level.

ADR-003 already froze **`tenant_id`** (+ `parent_org_id` for MSP, + `region` for residency) as **certificate-bound** identity claims, with server-side isolation as *shared-schema + `tenant_id`-leading composite keys + Postgres RLS* (enforced in Sprint 2; the binding frozen in Sprint 1). What ADR-003 did **not** address is the **location/site** axis and where **human-user RBAC** lives. The agent ships *before* the SaaS exists, so any binding that must be cryptographic has to be frozen now — but over-freezing a *mutable* attribute into the certificate would recreate the exact "fleet-wide flag-day" trap ADR-003 warns about (re-issuing every cert to change a value). This ADR settles the location axis before S1-T14 builds the enrollment workflow.

A senior multi-tenant alignment review (2026-06-09, ultracode adversarial verification: 3/4 load-bearing claims upheld, 1 refined) confirmed: location should be a **mutable, server-authoritative, intra-tenant attribute — never cert-bound**; the three user scopes are **server-side RBAC, entirely out of agent scope**; and `parent_org_id` future-proofs the MSP/reseller *billing* hierarchy but does **not** represent per-device location (the real gap to close).

## Decision

1. **Tenant boundary (unchanged).** `tenant_id` remains the **cryptographic isolation boundary**: immutable, certificate-bound (ADR-003), the leading column of every server-side composite key/index + RLS predicate, and the storage-key / dedup prefix. Cross-tenant access is a data breach and is prevented at the identity layer.

2. **Location/site boundary.** `location_id` is an **optional, mutable, server-AUTHORITATIVE intra-tenant attribute** used only as an **authorization scope filter** (dashboards/reports/devices/jobs/restores). A device may be reassigned between sites of the **same** tenant without re-enrolling. It is an **identifier only** — the location **name is never carried on the wire or persisted in agent state** (the server resolves `location_id → name`).

3. **Hard-boundary rule.** Location/site is a **soft** authorization scope, **not** a hard isolation boundary. When a set of devices/data must be **hard-isolated** (e.g. a reseller's mutually-distrusting end-customers), it is provisioned as a **separate tenant** (under the same `parent_org_id` if billed together) — **never** by hardening a location into an isolation boundary. This keeps exactly one hard boundary (`tenant_id`) and avoids a future second flag-day.

4. **`location_id` is NEVER certificate-bound.** Only `tenant_id` / `parent_org_id` / `region` are embedded in the agent certificate (ADR-003 §0.8). Binding a mutable, admin-reassignable attribute into an immutable credential is a category error: it would turn every device relocation into a re-enrollment / re-cert event — unacceptable for the hotel/SMB/MSP fleets where re-image and reassignment are routine.

5. **Server-authoritative location assignment.** The **server** is authoritative for a device's `location_id`. Two supply paths are supported, both server-validated:
   - **Operator hint (advisory):** an operator may choose a site at silent install; the agent sends it as the **advisory** `location_id` in `EnrollRequest` (analogous to the advisory `tenant_id`). The server **validates it against the tenant's authorized sites and MAY override or ignore it**.
   - **Token-scoped:** a Tenant Admin may mint a location-scoped enrollment token; the server assigns `location_id` from the token.
   Note: a per-**tenant** token (the common RMM/GPO mass-push case) cannot by itself identify a per-device location — hence the operator-hint path. In all cases the **authoritative** `location_id` is returned in `EnrollResponse` (and echoed in `RegisterResponse`), and the agent must never be able to self-assign into a site outside the Tenant Admin's grant.

6. **Human-user RBAC is server-side.** The Super Admin / Tenant Admin / Location-scoped-user model — roles, permissions, and user↔location grants — lives **entirely in the SaaS backend (Sprint 2+)**. The **agent authenticates no human user**; it holds only a device-scoped session token and its sole multi-tenant responsibility is to **produce and persist the device→tenant(→location) binding**. S1-T14 MUST NOT model roles, permissions, or user grants.

7. **Sprint-1 freeze (this ADR).** Frozen now: the decisions above + the reserved wire/state surface —
   - OpenAPI: a typed nullable `LocationId` schema; reserved `location_id` on `EnrollResponse` + `RegisterResponse` (server-authoritative) and an **advisory** optional `location_id` on `EnrollRequest`.
   - State: a non-secret `KeyLocationID` placeholder (identifier only).
   - `pkg/proto` regenerated to expose the typed field.
   **Deferred to Sprint 2 (server):** `location_id` minting, the `enrollment_token → location` mapping / location-scoped tokens, the `devices.location_id` column + RLS scope filter, location reassignment, all user RBAC, scope-filtered dashboards/reports, and (if ever needed) audit location attribution.

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| **Certificate-bind `location_id`** (like `tenant_id`) | Makes every intra-tenant device relocation a re-enrollment/re-cert flag day; binds a mutable admin-assigned attribute into an immutable credential (category error). The hard-boundary need is met by a separate tenant instead (Decision 3). |
| **Model location as a hard isolation boundary** | Two hard boundaries (tenant + site) doubles RLS/scope-filter surface and invites cross-axis leaks; a site that truly needs hard isolation should *be* a tenant. |
| **Reuse `parent_org_id` for location** | `parent_org_id` is a single upward MSP/billing pointer (LIC-6); it cannot represent a per-device site within a tenant. Orthogonal axes. |
| **Add nothing now; rely on `additionalProperties:true`** | The JSON envelope is forward-compatible, but the **closed state-store key allow-list** and the **immutable cert claim set** are not — leaving location unreserved risks an agent schema bump and keeps the cert flag-day risk open. Reserving a typed field + state key now is near-zero cost. |
| **Agent-authoritative location** | A device could self-assign into an arbitrary site, bypassing the Tenant Admin's selected-locations grant. Authority must stay server-side (Decision 5). |
| **Persist location name in agent state** | Names are mutable display data; the agent only needs the stable identifier. Storing the name invites staleness and leaks human-readable org data onto the device. Identifier-only (Decision 2). |

## Consequences

- **Positive:** exactly one cryptographic boundary (`tenant_id`); location is a cheap, mutable scope that supports admin reassignment without re-enrollment; the user-RBAC scope is cleanly server-side, keeping the agent thin; the wire/state surface is reserved so Sprint 2 adds location with no breaking change or fleet flag-day; the "hard boundary → new tenant" rule gives a clear escalation path for reseller isolation.
- **Negative / accepted:** until Sprint 2 ships RLS + the location scope filter, location-level isolation is only as strong as that server-side logic (acceptable — `tenant_id` remains the hard boundary regardless); the operator-hint path requires the installer/UX to optionally capture a site later (additive, non-breaking).
- **Sprint-1 impact:** `LocationId` schema + reserved fields in `EnrollRequest`/`EnrollResponse`/`RegisterResponse`; `state.KeyLocationID` (non-secret); `pkg/proto` regenerated; tests for the wire round-trip and the state key. No enforcement, no RBAC, no T14 yet.
