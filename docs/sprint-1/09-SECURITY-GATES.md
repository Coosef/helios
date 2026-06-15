# S1-T32 — Security controls, negative-test gating, and required checks

> Status: implemented (test/CI/docs only; no new production security logic).
> Suite: [`test/security/`](../../test/security/) (theme-G negative suite).
> Gate: `task test:negative` → the **blocking** CI `security` job.

The security controls are built across Sprints 1's tasks; T32 **proves them
end-to-end with a consolidated negative suite** and makes that suite (and the
secret scanner) **required, non-bypassable merge checks**.

## Theme-G negative suite (`test/security/`, runs under `go test ./...`)

Each test RE-EXERCISES a control end-to-end and asserts the FAIL-CLOSED outcome:

| Test | Control (AC) | Proves |
|---|---|---|
| `TestNeg_SecretsNeverLogged` | redaction (AC-12) | after a real enroll+heartbeat, the token + bearer appear **0** times in `agent.log` and `security.log` |
| `TestNeg_SPKIPinMismatchRefused` | SPKI pinning (AC-34) | a non-pinned cert is refused at the TLS handshake (the request never reaches the server) |
| `TestNeg_TokenReplay409FailsClosed` | enrollment (AC-15) | a replayed (409) token fails closed (terminal) and does **not** overwrite the existing cert/device_id |
| `TestNeg_ClonedStateDetectable` | clone detection | the (fakeSaaS) server flags a clone via 409; the agent surfaces it terminally and persists nothing |
| `TestNeg_ManifestRejectionsNothingSwapped` | update trust (AC-23/24/25/29) | forged-signature / revoked-key / hash-mismatch / downgrade manifests are rejected by the **real FSM** and the untrusted artifact never becomes the live binary (no `update.swapped`) |
| `TestNeg_UnknownSigningKeyRejectedNothingSwapped` | update trust (AC-23) | an otherwise-valid manifest signed by a key_id **not in the trust anchor** is rejected by the **real FSM** with `trust.ErrUnknownKey` (distinct from revocation) and nothing is staged/swapped |
| `TestNeg_StateProtectorTamperFailsClosed` | DPAPI/state (AC-17) | a tampered protected blob fails closed (auth-tag), not garbage |
| `TestNeg_NoReturnTrueVerificationStub` | static guard (AC-23/35) | `verify.go` calls the real `ed25519.Verify` with **no `return true` stub**; the embedded keyset is public-only |

No external network: `httptest` + `mocksaas` + `fakeSaaS` only; deterministic
signed fixtures; temp dirs; logs flushed before grep.

## Running the gate

```
task test:negative
```

runs the consolidated suite **plus** the security-critical packages whose negative
tests must gate merges (`verify`, `manifestcheck`, `trust`, `httpclient`,
`logging`, `state`, `enroll`).

## Required CI checks (branch protection)

The following CI jobs **must be configured as required, non-bypassable checks**
on the protected `main` branch:

| Required check | Job |
|---|---|
| `test` | unit tests + coverage gate (AC-38: ≥85% security pkgs, ≥70% overall) |
| **`security`** | the theme-G negative gate (`task test:negative`) |
| `gitleaks` | secret + private-key scan (AC-35) |
| `lint` | golangci-lint (gosec + staticcheck) |
| `race` | `go test -race ./...` |
| `contract` | OpenAPI/Prism contract + signed fixtures |
| `drift` | codegen / fixture / go.mod tidy drift |
| `cross` | windows/amd64 + linux/amd64,arm64 build |
| `govulncheck` | vulnerable-dependency scan |

> **GitHub branch protection is a repository setting and must be enabled
> manually** (Settings → Branches → branch protection rule for `main` → "Require
> status checks to pass"), then the checks above selected. The CI workflow
> provides the named, blocking jobs; it cannot make itself *required* — that is a
> maintainer action. The Windows/Pester negatives (`windows-test`, `pester`,
> `installer-build`, `sign-smoke`) remain **non-blocking** until stabilized, then
> are promoted.

## Secrets rule reconciliation (CLAUDE.md)

CLAUDE.md says *"Always use environment variables for sensitive configuration."*
In this codebase that rule means: **no secrets in source or in `config.yaml`.**
Runtime secrets are supplied at run time from the environment, an ACL-locked
file, or a secrets manager — never committed and never logged:

- the **enrollment token** comes from `BEYZ_ENROLLMENT_TOKEN` or the installer
  one-shot file (`state\enroll-token`), never `config.yaml`;
- the **agent key / cert / license** live DPAPI-wrapped in the ACL-locked state
  store, not in source/config;
- the **update-signing private key** lives in the CI secret manager; the repo
  embeds only the **public** keyset;
- the **Authenticode certificate** (T30) comes from a protected GitHub
  Environment at release time.

`config.yaml` carries only non-secret operational settings (the schema rejects an
`enrollment_token` key), and the `Secret` wrapper redacts secret values at every
log sink (`***REDACTED***`).
