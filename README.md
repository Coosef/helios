# Helios

**Helios Data Protection Platform** — a product of **Beyz System A.Ş.**

A SaaS-based **hybrid data protection platform** with Windows/Linux agents — a lightweight alternative to Veeam/Acronis for SMBs, hotels, MSPs, and multi-location companies. The product provides a cloud management panel, customer-owned **and** Helios Cloud storage, client-side encryption, device + storage-quota licensing, and signed agent auto-update.

> **Status: Sprint 1 (Agent Foundation) — P0 complete.**
> This repository contains the agent/updater **foundation**: the Windows service, config, structured
> logging + hash-chained audit, the protected state store, identity/enrollment, the hardened
> SPKI-pinned transport, heartbeat/task-poll, the real enforcing Ed25519 updater (verify → stage →
> swap → health-gate → rollback), the Inno Setup installer, and the CI security gates. No backup,
> restore, compression, or encryption **engine** exists yet (those are Sprints 3–6). See
> [`docs/sprint-1/IMPLEMENTATION-PLAN.md`](docs/sprint-1/IMPLEMENTATION-PLAN.md) for the task tracker
> and [`docs/design/`](docs/design/) for the Sprint-1 design records (DR-01…06).

## Components (this repository)

- **`beyz-backup-agent`** — the Windows/Linux backup agent (runs as a Windows Service; Linux systemd prep).
- **`beyz-backup-updater`** — the **on-demand** updater binary (NOT a persistent service — Technical Design §0.6).

## Prerequisites

| Tool | Version | Needed for |
|------|---------|-----------|
| Go | **1.23.x** | building/testing the agent and updater |
| [go-task](https://taskfile.dev) | 3.x | the `task` runner (optional but recommended) |
| [golangci-lint](https://golangci-lint.run) | 1.59+ | linting (`task lint`) |
| Inno Setup | 6.x | building the Windows installer (S1-T29, Windows only) |

## Build, run, test

Using go-task:

```sh
task build         # build agent + updater into ./dist
task test          # go test ./...
task test:race     # race detector
task test:negative # theme-G security negative suite (the CI `security` gate)
task contract      # OpenAPI contract tests against the Prism mock + signed update fixtures
task lint          # golangci-lint (gosec + staticcheck)
task cross         # cross-compile windows/amd64 + linux/amd64,arm64 (versioned)
task dist          # produce dist artifacts + SHA256SUMS
task run:agent -- --version
task --list        # show all targets
```

The coverage gate (`scripts/coverage_gate.sh`, run in CI) enforces ≥70% overall and ≥85% on the
security-critical packages.

Equivalent raw Go commands (no go-task required):

```sh
go build ./...                          # build everything
go test ./...                           # run all tests
go run ./cmd/agent --version            # print agent build info
go run ./cmd/updater --version          # print updater build info
```

Version metadata is injected at build time via `-ldflags` into
`internal/buildinfo` (see `Taskfile.yml`).

## Repository layout

```text
beyz-backup/
├── go.mod                      # module github.com/beyzbackup/beyz-backup (Go 1.23)
├── Taskfile.yml                # build / test / lint / dist / installer targets
├── .golangci.yml               # lint config (gosec is a required gate)
├── cmd/
│   ├── agent/                  # → beyz-backup-agent
│   └── updater/                # → beyz-backup-updater (on-demand)
├── internal/
│   ├── buildinfo/              # version/build metadata (ldflags-injected)
│   ├── agent/                  # app, config, state, identity, enroll,
│   │                           #   heartbeat, tasks, license, service, audit
│   ├── updater/                # app, manifestcheck, verify, trust, swap, healthgate
│   └── transport/              # httpclient (TLS+SPKI pin), saasclient
├── pkg/                        # shared kernel: proto, wireversion, manifest,
│   │                           #   hashing, storage
├── api/                        # OpenAPI spec + update-manifest schema (source of truth)
├── configs/                    # config.sample.yaml + config.schema.json
├── build/                      # windows/, linux/ (systemd), keys/ (PUBLIC test key only)
├── installer/                  # Inno Setup script + helpers
├── test/                       # contract / integration / fixtures
└── docs/                       # PRD, ARCHITECTURE, SECURITY, ROADMAP, ADRs, sprint-1/
```

Packages are added as their tasks land; see the status tracker in
[`docs/sprint-1/IMPLEMENTATION-PLAN.md`](docs/sprint-1/IMPLEMENTATION-PLAN.md).

## Security notes (summary)

Full detail in [`docs/SECURITY.md`](docs/SECURITY.md) and the ADRs.

- **No secrets in source.** Private keys, tokens, and the update-signing **private** key are never committed; only the **public** update-signing test key (`build/keys/update_pub_test.pem`, added in S1-T22) is in the repo. `.gitignore` blocks `*.pem`/`*.key`/`*.pfx`/`*.env` with an explicit allow for the public key.
- **Encryption & recovery:** Escrowed Recovery by default, optional Zero-Knowledge mode — [ADR-001](docs/ADR/ADR-001-Encryption-Recovery-Model.md).
- **Update trust:** real Ed25519 manifest signature + BLAKE3/SHA256 hash + anti-rollback, verified enforcing (never stubbed) — [ADR-002](docs/ADR/ADR-002-Update-Signing-Trust-Model.md).
- **Identity:** server-issued opaque `device_id` + persisted GUID + tenant-bound cert — [ADR-003](docs/ADR/ADR-003-Agent-Identity-Model.md).
- **Protocol:** versioned (`/v1/`, version headers, `426` floor, forward-compatible envelopes) — [ADR-004](docs/ADR/ADR-004-Protocol-Versioning.md).
- **Control channel:** TLS 1.2+/1.3 with mandatory **SPKI pinning** (fail-closed) — [ADR-005](docs/ADR/ADR-005-Control-Channel-TLS-Pinning.md).
- **Tenancy:** immutable `tenant_id`/`parent_org_id`/`region` bound in the cert; `location_id` advisory — [ADR-006](docs/ADR/ADR-006-Location-Site-Scoping-and-RBAC-Boundary.md).
- **At rest & audit:** secrets DPAPI-wrapped in an ACL-locked store; tamper-evident BLAKE3 audit chain. See the design records below.

## Documentation

- Install & operate: [`docs/INSTALL.md`](docs/INSTALL.md)
- Product & architecture: [`docs/PRD.md`](docs/PRD.md), [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/SECURITY.md`](docs/SECURITY.md), [`docs/ROADMAP.md`](docs/ROADMAP.md)
- **Design records (Sprint 1, as-built):** [`docs/design/`](docs/design/) — [DR-01 Key Management](docs/design/DR-01-key-management.md), [DR-02 Enrollment & Identity](docs/design/DR-02-enrollment-and-identity.md), [DR-03 Update Trust & Signing](docs/design/DR-03-update-trust-and-rotation.md), [DR-04 Tenancy & Isolation](docs/design/DR-04-tenancy-and-isolation.md), [DR-05 Protocol Versioning](docs/design/DR-05-protocol-versioning.md), [DR-06 Audit Event Schema](docs/design/DR-06-audit-event-schema.md)
- Decision records: [`docs/ADR/`](docs/ADR/) (ADR-001…006)
- Sprint 1 package: [`docs/sprint-1/`](docs/sprint-1/) (technical design, task breakdown, acceptance criteria, open questions, implementation plan, risk register, CI, installer, signing, security gates)
- Development rules: [`CLAUDE.md`](CLAUDE.md)
