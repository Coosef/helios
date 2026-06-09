# ADR-002 — Update Signing & Trust Model

- **Status:** Accepted
- **Date:** 2026-06-08
- **Deciders:** Chief Architect, Security Lead
- **Related:** OQ-03 / OQ-04 / OQ-19, Technical Design §0.5 / §0.6 / §4, Risks UPD-1 / UPD-2 / UPD-6 / UPD-8 / SEC-3 / SEC-9 / ARCH-4 / GAP-3
- **Note:** Production signing-key custody (HSM/KMS) procurement runs in parallel; Sprint 1 verifies against an embedded **test** public key.

## Context

`beyz-backup-updater.exe` replaces the agent binary that runs as **`LocalSystem`** on every unattended endpoint. The update channel is therefore the **highest-value supply-chain target** in the system: whoever can get a binary accepted by the updater obtains SYSTEM-level code execution across the entire fleet — the textbook ransomware vector against a backup product.

The embedded trust anchor and the verification code path become **immutable the moment the first installer ships** (auto-update guarantees a long-lived, mixed-version fleet that cannot be flag-day-migrated). CLAUDE.md explicitly forbids placeholder security implementations and forbids removing update-signature validation. PRD req 12 calls the updater "placeholder logic," which we interpret (per §0.6) as *orchestration stubbed, cryptography real and enforcing*.

## Decision

1. **Signature algorithm: Ed25519** over the **entire canonicalized manifest** (RFC 8785 JSON Canonicalization Scheme), not just the binary hash. Small keys/signatures, fast, no padding/parameter pitfalls.

2. **Trust anchor: a compile-time embedded public-key *set*** in the updater binary (not a single key, not a config-file key). The manifest carries a **`key_id`** selecting which embedded key signed it; the updater accepts a signature from **any embedded, non-revoked** key. This enables overlapping-key rotation without re-issuing the fleet.

3. **Revocation:** every manifest carries a (possibly empty) **`key_revocation_list`**; a signature from a revoked `key_id` is rejected.

4. **Private-key custody:** the signing private key lives in an **offline HSM or cloud KMS** that only the release pipeline can *invoke*, never export. It is **never** on CI runners, developer laptops, or in the repository. Signing is a **multi-person, logged ceremony**. (Procurement of production custody runs in parallel with Sprint 1; see Consequences.)

5. **Hash validation:** the manifest carries **both BLAKE3 and SHA256** of each artifact, algorithm-tagged (`blake3:` / `sha256:`). The expected hash is read **only from the signature-verified manifest** — never from a server header, filename, or unsigned source (closes the "SHA256 theatre" gap). The downloaded bytes are hashed and compared on the **exact, locked file handle** that will be used (verify-then-exec, closing TOCTOU).

6. **Anti-rollback:** the manifest carries a monotonic `target_version` and a `min_supported_version` floor. The updater **refuses** any `target_version ≤ persisted current_version` unless a separately, explicitly-signed emergency-downgrade flag is present. Comparison uses the **signed manifest version**, never a filename or header.

7. **Verification is REAL and enforcing in Sprint 1**, exercised against an **embedded test public key** committed as `build/keys/update_pub_test.pem` (public key only — the private test key lives in the CI secret manager, never the repo). A `return true` stub is forbidden.

8. **Transport & integrity of the swap:** the manifest is fetched over the **SPKI-pinned** SaaS control channel; the agent-binary swap is an atomic `MoveFileEx` rename with an integrity-checked `.bak` rollback. The updater is an **on-demand binary, not a second persistent service** (per §0.6); two-stage updater self-update and the persistent watchdog are deferred to Sprint 8.

## Alternatives Considered

| Alternative | Why rejected |
|---|---|
| **RSA-PSS / RSA-4096** with the same custody | Workable, but larger keys/signatures and padding/parameter footguns; Ed25519 is simpler and safer with no practical downside for this use. |
| **No-op verify stub in Sprint 1, real verification in Sprint 8** | Directly violates CLAUDE.md ("no placeholder security," "never remove update signature validation"). Ships a permanent unverified-SYSTEM-code-execution path; once a fleet trusts a no-op updater you cannot retrofit enforcement without an out-of-band re-install. |
| **Sign only the binary hash / leave the manifest unsigned** | Lets an attacker tamper with version, rollout, kill-switch, or hash-source fields; if the hash can come from outside the signed document the SHA256 check is theatre. The whole canonical manifest must be signed. |
| **Single embedded key (no `key_id`/revocation)** | Unrotatable once the fleet is deployed; a compromised or expiring key becomes an unrecoverable fleet event. A key *set* + revocation list is nearly free now. |
| **Pin the signing key in config/state instead of compiling it in** | A config-resident trust anchor can be swapped by anyone who can write config; compiling it into the (Authenticode-signed) binary makes it un-swappable without replacing a signed binary. |

## Consequences

**Positive**
- Tamper-proof, rotatable, revocable update trust from the first installer; anti-rollback closes downgrade attacks; verify-then-exec closes TOCTOU.
- Defense in depth: Authenticode (OS distribution identity) is separate from the Ed25519 manifest signature (in-product update trust root).

**Negative / costs**
- Operational burden of an offline/HSM signing ceremony and key-rotation discipline.
- A bad or expired embedded key/manifest is fleet-affecting, so key rotation and an out-of-band recovery path **must** be documented (handled by §0.5 pin/key rotation guidance and REV-2).

**Sprint-1 impact**
- `internal/updater/verify` + `internal/updater/trust` implement real Ed25519 + BLAKE3/SHA256 verification against the embedded **test** key (S1-T22/T23).
- `api/manifest.schema.json` (S1-T21) freezes the signed manifest format with `key_id`, version floor, revocation list, and per-artifact dual hashes.
- **Procurement (external lead-time, started now):** production Authenticode certificate and HSM/KMS signing-key custody. Sprint-1 builds use test keys; production keys are required before the first signed public release.
