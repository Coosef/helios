# Deployment Notes

> Status: forward-looking note (S1-T03). **No deployment artifacts are implemented in Sprint 1.**

## Agent ↔ SaaS communication

The agent communicates with a **remote SaaS control-plane over HTTPS**. The base
URL is operator-configured (`server.url` in `config.yaml`, default
`https://api.beyzbackup.com`) and is **never assumed to be local**. The generated
client (`pkg/proto`, S1-T03) is transport-agnostic: `proto.NewClient(server, …)`
accepts any HTTPS endpoint, and TLS 1.2+/SPKI-pinning hardening is layered on in
S1-T12 (`internal/transport/httpclient`). The Prism mock used in development
(`task spec:mock`) is a **local stand-in for tests only** — it does not change
the agent's remote-endpoint model.

## Backend deployment (future sprints)

The SaaS backend (FastAPI + PostgreSQL + Redis) is **Sprint 2+** and is **not**
implemented or containerized in Sprint 1. It is being designed to be
**deployment-friendly and cloud-native**:

- **Docker Compose** for local/integration environments (API + PostgreSQL +
  Redis + the OpenAPI-driven mock/contract harness).
- **Kubernetes-ready** for production (stateless, horizontally scalable API pods
  behind an ingress with TLS; PostgreSQL and Redis as managed/operator-run
  services; per-tenant data residency per ADR-003 / REV-5).

Because the agent↔server contract is the committed OpenAPI 3.1 spec
(`api/openapi.yaml`, ADR-004) and the client is generated from it, the backend
can be implemented and deployed independently in any environment without
changing the agent — the only coupling is the versioned HTTPS contract.

No Docker/Kubernetes manifests are added in this task; they will be introduced in
the relevant backend sprint.
