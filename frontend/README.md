# Helios Console — frontend (UI Sprint 1: product shell)

Operator console for the **Helios Data Protection Platform** (a product of **Beyz System A.Ş.**).
This is **UI Sprint 1** — the product shell, ported from the design prototype, running on
**mock fixtures only**. There is no backend integration yet.

## Design source

Ported from the design package in [`../Backup/`](../Backup/) (a no-build, browser-global React
prototype). The visual system (`Backup/styles.css`) is reused verbatim as
[`app/_design-tokens.css`](app/_design-tokens.css); the prototype's `window.*` modules and
in-browser Babel runtime were **not** carried over. The original `Backup/` folder is the design
source of truth and is never modified or imported at runtime.

## Status & limitations (read this first)

- **Mock-only.** All data comes from typed fixtures in [`lib/fixtures.ts`](lib/fixtures.ts), read
  exclusively through the API facade in [`lib/api.ts`](lib/api.ts). No real network calls.
- **No backend / no Sprint-2 work.** There is no SaaS/management API yet; the only real Helios
  contract today is the agent control-plane (`api/openapi.yaml`), which is not a UI API.
- **Phase markers are intentional.** Screens that depend on capabilities the backend does not have
  yet show a banner:
  - **Backend pending** — backup jobs, job details, restore, storage, Helios Cloud, alerts,
    reports (engines land in Sprints 3–7 / SaaS in Sprint 2).
  - **Future preview** — Helios Intelligence (AI), executive analytics (no AI backend).
- **Advisory licensing.** The Licensing screen is advisory-only (S1-T17): claims are shown but
  nothing is enforced/blocked.
- **Linux is prep-only.** Linux devices are shown as prep-only (the Linux secret protector lands in
  Sprint 8); enrollment is not functional on Linux yet.
- **Agent Updates** reflects the real updater chain: verify → decide → stage → swap → health gate →
  rollback (T21–T27). **Audit** uses the frozen DR-06 event taxonomy and is hash-chained.
- **RBAC / tenant / super-admin** gating is **UI-only mock**. The SaaS backend will be the
  authoritative authorization source.

## Architecture

- **Next.js (App Router) + TypeScript + React 18.**
- **Routing:** tenant plane under `app/(app)/*`; super-admin control plane under `app/(app)/super/*`;
  login at `app/login`. The shell (`components/AppShell.tsx`) renders the sidebar/topbar and picks
  the tenant vs control-plane nav from the route.
- **Design tokens:** CSS variables in `app/_design-tokens.css` (+ small additions in
  `app/globals.css`); theme/density are token-driven.
- **API boundary:** `lib/api.ts` exposes the `HeliosApi` interface; today only the `mockApi`
  implementation exists. When the management **OpenAPI** backend lands, generate a typed client that
  implements `HeliosApi` and return it from `getApi()` — **no screen changes required**. Screens
  import data only through `getApi()`, never from `lib/fixtures` directly.
- **i18n:** EN/TR/DE scaffold (`lib/i18n.tsx`) with graceful fallback; switchable from the top bar.

## Run / build / validate

```sh
cd frontend
npm install        # one-time
npm run dev        # http://localhost:3000  (redirects to /login)
npm run build      # production build (all routes)
npm run typecheck  # tsc --noEmit
npm run lint       # eslint (next/core-web-vitals)
npm test           # node --test data-layer smoke tests
```

## Future management-API integration

1. Author the management OpenAPI spec (the UI contract) — Sprint 2.
2. Generate a typed client implementing `HeliosApi`.
3. Swap it into `getApi()`; keep `mockApi` for tests/Storybook.
4. Add auth/session (the SaaS backend; never the agent's SPKI-pinned channel) and enforce
   RBAC/tenant scoping server-side.
