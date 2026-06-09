# ADR-005 — Control-Channel TLS Termination & SPKI Pinning

- **Status:** Accepted
- **Date:** 2026-06-09
- **Deciders:** Chief Architect
- **Related:** Technical Design §0.5 / §0.9, S1-T12 (`internal/transport/httpclient`), S1-T13 (saasclient), S1-T17 (heartbeat/poll), OQ-26 / OQ-27, Risks SEC-4 / REV-2 / GAP-8 / SCALE-1 / SCALE-3, ADR-004 (Protocol Versioning)

## Context

S1-T12 (the hardened control-channel HTTP client) passed a senior architecture review and was **approved with conditions**. The client makes **SPKI public-key pinning the trust anchor** for the agent→SaaS control channel (SEC-4): with `InsecureSkipVerify=true` it verifies the server **leaf** certificate's `sha256(SubjectPublicKeyInfo)` against a configured **pin set**, plus the certificate validity window and host identity. This is correct and MITM-resistant, but it **couples the entire fleet to how the SaaS terminates TLS on the control-channel endpoint**:

- Pinning targets the **leaf public key**. If the control-channel endpoint sits behind a **managed/CDN TLS terminator** (Cloudflare, AWS ALB managed certs, etc.) that **auto-rotates the leaf key**, the pin eventually stops matching and **every agent loses connectivity at once (fleet lockout)** — a routine, provider-driven cert change becomes a fleet-down event on a *data-protection* product.
- A server can **renew its certificate with the same key** without breaking pins (the pin is the *key*, not the cert); only **key rotation** breaks pins. Pinning therefore requires an operational commitment to a **stable, Beyz-controlled key** (or a small managed key-set).
- The pin set is captured at client construction; **runtime pin reload is not implemented** in Sprint 1, so a delivered rotation pin takes effect only on agent restart.
- Pinning intentionally defeats **TLS-intercepting (SSL-inspection) corporate proxies**, and the stdlib transport cannot perform **NTLM/Kerberos** proxy authentication — real enterprise-network constraints.

These are not T12 code defects; they are **system-level decisions T12 forces**, and they must be recorded before Sprint 2 builds the server and before any production rollout.

## Decision

1. **T12 is approved with conditions** (recorded here). The transport is production-grade; the conditions are server / operational / integration obligations.

2. **Stable control-channel TLS key.** The Beyz Backup SaaS control channel MUST present a **stable, Beyz-controlled TLS leaf key**, or a **small managed key-set explicitly designed for SPKI pinning** (every key in the set published as a pin).

3. **No auto-rotating CDN / managed TLS leaf keys** for the pinned control-channel endpoint.

4. **CDN / managed-TLS in front of the public app is allowed**, but the agent control-channel endpoint MUST either:
   - terminate TLS on a **stable Beyz-controlled leaf key**, or
   - be a **dedicated agent API endpoint** with stable SPKI-pinning semantics, separate from the browser-facing app behind the CDN.

5. **Runtime pin reload is NOT required in Sprint 1.** Accepted current behavior: **pin rollover requires an agent restart** (the pin set is read at startup from the compiled-in bootstrap pin ∪ the ACL-locked state store, §0.5). A **dynamic pin provider / runtime reload is deferred to Sprint 8.**

6. **Forward requirement:** a **fleet pin-rollover runbook** MUST be **written and tested before production release** (deliver overlapping pin → confirm fleet adoption → rotate key, with the installer-driven re-pin as the documented last resort). Tracked as **OQ-26**.

7. **Enterprise-network limitations (documented constraints — OQ-27):**
   - **TLS inspection / SSL interception breaks SPKI pinning by design** — the agent control-channel FQDN MUST be **bypassed (allow-listed)** in the inspection proxy.
   - **NTLM / Kerberos authenticated proxy is NOT supported in Sprint 1.**
   - **Basic proxy** via the system environment proxy or an explicit proxy URL **IS supported**.

8. **T13 integration contract** (`internal/transport/saasclient`):
   - `ServerName` MUST be derived from the **`api_base_url` host** (required for IP-literal endpoints; SNI covers DNS endpoints).
   - `TokenProvider` MUST return an **in-memory cached** session token — **no DPAPI unwrap per request**.
   - **401 Unauthorized** MUST be handled by the caller (token refresh / re-enroll path).
   - **426 Upgrade Required** MUST be handled by the caller (stop calling, route to updater — ADR-004).
   - **No duplicate version-header request editors** — T12 already injects `X-Agent-Version` / `X-Protocol-Version`.
   - **T12 is control-channel only — it MUST NOT be used for large backup-payload transfer** (bulk data goes through `pkg/storage`; T12 buffers bodies for retry replay).

9. **T17 heartbeat / task-polling requirements:** cadence **jitter**; honor the server **Retry-After**; **low retry count** for heartbeat/poll; **circuit-breaker** behavior; **recovery thundering-herd mitigation**.

10. **Sprint 2 backend requirement:** under overload the SaaS API MUST prefer **`429 + Retry-After`** over generic **5xx**, so the client's Retry-After honoring controls load instead of the per-call retry (≤5×) **amplifying** it across a large fleet.

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| Pin an **intermediate / root CA** instead of the leaf key | More rotation-tolerant, but with `InsecureSkipVerify=true` the chain is not validated, so intermediate pinning is unreliable; it also weakens the anchor (any cert under that CA becomes trusted). Leaf-key pinning + a stable Beyz key is the stronger, simpler model. |
| **Drop pinning**, rely on system-CA trust + bearer token | Rejected by SEC-4: a single mis-issued / compromised CA cert MITMs the entire fleet on a data-protection product. |
| Implement **runtime pin reload now** (Sprint 1) | Real, but not Sprint-1-critical; rollover-by-restart is acceptable for the foundation. Deferring to Sprint 8 avoids a dynamic-provider refactor under time pressure. |
| Allow **auto-rotating CDN/managed leaf keys** + periodic re-pin via update | Turns every provider cert rotation into a forced fleet-update window; fragile and operationally unacceptable for a backup product that must stay reachable. |

## Consequences

- **Positive:** the fleet has a clear, enforceable trust model; Sprint 2 server work inherits an explicit TLS-key constraint; T13/T17 have a written integration contract; enterprise-deployment limits are known up front; the 429-vs-5xx requirement caps fleet retry amplification.
- **Negative / accepted:** pinning blocks TLS-inspection environments (FQDN allow-list required) and NTLM-proxy environments (unsupported in Sprint 1); pin rollover needs an agent restart until Sprint 8.
- **Release blockers (production fleet):** Decisions **#2 / #3 / #4** (server TLS-key stability) and **#6** (tested rollover runbook) are **release blockers**. For a single controlled pilot where Beyz operates the SaaS, they reduce to operational pre-conditions rather than hard blockers.
