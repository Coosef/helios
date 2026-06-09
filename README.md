# Beyz Backup

A SaaS-based **hybrid backup platform** with Windows/Linux agents — a lightweight alternative to Veeam/Acronis for SMBs, hotels, MSPs, and multi-location companies. The product provides a cloud management panel, customer-owned **and** Beyz Cloud storage, client-side encryption, device + storage-quota licensing, and signed agent auto-update.

> **Status: Sprint 1 (Agent Foundation) — in progress.**
> This repository currently contains the agent/updater **foundation** only: project scaffold, build tooling, and the binaries' entrypoints. No backup, restore, compression, or encryption engine exists yet (those are Sprints 3–6). See [`docs/sprint-1/IMPLEMENTATION-PLAN.md`](docs/sprint-1/IMPLEMENTATION-PLAN.md) for the live task status.

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
task lint          # golangci-lint
task run:agent -- --version
task --list        # show all targets
```

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

## Documentation

- Product & architecture: [`docs/PRD.md`](docs/PRD.md), [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), [`docs/SECURITY.md`](docs/SECURITY.md), [`docs/ROADMAP.md`](docs/ROADMAP.md)
- Decision records: [`docs/ADR/`](docs/ADR/)
- Sprint 1 package: [`docs/sprint-1/`](docs/sprint-1/) (technical design, task breakdown, acceptance criteria, open questions, implementation plan, risk register)
- Development rules: [`CLAUDE.md`](CLAUDE.md)
