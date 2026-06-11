// Package manifestcheck fetches the signed update manifest over the SPKI-pinned
// control channel and makes the update DECISION (S1-T24, ADR-002): signature
// verification (via internal/updater/verify), anti-rollback, the min-supported
// floor, the kill-switch, and artifact selection. It returns a typed Decision.
//
// SCOPE (frozen): this is a PURE decision layer. It does NOT read updater_state.json
// or buildinfo (the current-version baseline is an input parameter supplied by the
// orchestrator, T27), does NOT emit audit events (T27), does NOT download the binary
// or verify its hash (T25), does NOT swap (T25), health-gate (T26), or drive the FSM
// (T27). Every error or ambiguity fails closed to "do not update".
//
// Authoritative inputs only: the version, kill-switch, cohort, and downgrade flags
// are read EXCLUSIVELY from the signature-verified manifest — never from an unsigned
// header, filename, or task payload.
//
// Replay boundary (FI-T24-1): anti-rollback here is purely target_version vs the
// supplied baseline. It does NOT defend against replay of a wholly-authentic but
// stale/recalled manifest (e.g. a signed 2.0.0 later recalled by 2.0.1 and replayed
// to a 1.x agent) — that requires persistent freshness state (released_at
// monotonicity or a high-water mark) owned by the orchestrator (T27). The signed
// `released_at` is carried for that future use; it is not consulted here.
package manifestcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// DefaultManifestMaxBytes bounds a fetched manifest (T13-C3). A signed manifest is
// small (KB-scale); 1 MiB is generous headroom while still capping memory.
const DefaultManifestMaxBytes int64 = 1 << 20

// Machine-readable decision reasons (mapped to update.* audit events by T27).
const (
	ReasonOK               = "ok"
	ReasonFetchFailed      = "fetch_failed"
	ReasonManifestRejected = "manifest_rejected"  // signature/parse/trust failure (verify)
	ReasonDowngradeBlocked = "downgrade_blocked"  // target_version <= current (no signed downgrade)
	ReasonBelowFloor       = "below_floor"        // current_version < min_supported_version
	ReasonUpdateNotAllowed = "update_not_allowed" // signed kill-switch update_allowed=false
	ReasonNoArtifact       = "no_artifact"        // no artifact for this platform/arch
)

var (
	// ErrFetch wraps any failure to fetch the manifest (network, pin mismatch,
	// non-200 status, oversized body). A pin mismatch is a MITM signal.
	ErrFetch = errors.New("manifestcheck: manifest fetch failed")
	// ErrDowngradeBlocked is returned when target_version <= current_version and no
	// signed emergency-downgrade (allow_downgrade) authorizes it.
	ErrDowngradeBlocked = errors.New("manifestcheck: downgrade blocked (anti-rollback)")
	// ErrBelowFloor is returned when current_version is below the manifest's
	// min_supported_version (the agent is too old to apply this update directly).
	ErrBelowFloor = errors.New("manifestcheck: current version below min_supported_version")
	// ErrUpdateNotAllowed is returned when the SIGNED manifest disables updates
	// (update_allowed=false kill-switch).
	ErrUpdateNotAllowed = errors.New("manifestcheck: update_allowed is false (kill-switch)")
	// ErrNoArtifact is returned when the manifest has no artifact for the target
	// platform/arch.
	ErrNoArtifact = errors.New("manifestcheck: no artifact for platform/arch")
	// ErrManifestRejected wraps a verification failure (signature/parse/trust). The
	// underlying verify.* / trust.* sentinel is preserved via errors.Is.
	ErrManifestRejected = errors.New("manifestcheck: manifest rejected by verification")
)

// Decision is the typed result of evaluating an update manifest. It is always
// non-nil. Proceed is true IFF the returned error is nil.
type Decision struct {
	// Proceed reports whether the update should be applied. Proceed == (err == nil).
	Proceed bool
	// Reason is a machine-readable decision reason (see Reason* constants).
	Reason string
	// Manifest is the signature-verified manifest (nil when verification failed).
	Manifest *manifest.Manifest
	// Artifact is the artifact selected for the target platform/arch (zero value
	// unless Proceed is true).
	Artifact manifest.Artifact
	// CurrentVersion is the baseline used for the anti-rollback decision.
	CurrentVersion manifest.Version
	// TargetVersion is the parsed manifest target_version (zero value if unparsed).
	TargetVersion manifest.Version
	// EmergencyDowngrade reports whether a signed allow_downgrade authorized a
	// target_version below the current version.
	EmergencyDowngrade bool
	// RolloutCohortPct is the manifest's rollout cohort percentage, parsed and
	// exposed but NOT enforced in Sprint 1 (membership is deferred — UPD-5).
	RolloutCohortPct *int
}

// Evaluate is the pure update decision: it verifies the manifest signature (T23),
// then applies anti-rollback, the min-supported floor, the kill-switch, and
// artifact selection against the supplied current-version baseline. It returns a
// Decision and a typed error; err == nil IFF the update should proceed. It performs
// no I/O and reads no state — the baseline and platform/arch are inputs.
func Evaluate(rawManifest []byte, keys *trust.KeySet, baseline manifest.Version, platform, arch string) (*Decision, error) {
	d := &Decision{CurrentVersion: baseline}

	// 1. Verify the signature (and structure) — the only source of trust. Any
	//    failure (bad signature, unknown/revoked key, malformed/duplicate manifest)
	//    fails closed.
	m, err := verify.Manifest(rawManifest, keys)
	if err != nil {
		d.Reason = ReasonManifestRejected
		return d, fmt.Errorf("%w: %w", ErrManifestRejected, err)
	}
	d.Manifest = m
	d.RolloutCohortPct = m.RolloutCohortPct // parsed + exposed, not enforced (freeze #6)

	// Re-parse the (already schema-validated) versions for comparison.
	target, err := manifest.ParseVersion(m.TargetVersion)
	if err != nil {
		d.Reason = ReasonManifestRejected
		return d, fmt.Errorf("%w: target_version: %w", ErrManifestRejected, err)
	}
	floor, err := manifest.ParseVersion(m.MinSupportedVersion)
	if err != nil {
		d.Reason = ReasonManifestRejected
		return d, fmt.Errorf("%w: min_supported_version: %w", ErrManifestRejected, err)
	}
	d.TargetVersion = target

	// 2. Anti-rollback: reject target_version <= current_version, unless a strict
	//    downgrade is authorized by the SIGNED allow_downgrade flag (Sprint-1
	//    emergency downgrade; a separate root signature is deferred).
	switch cmp := target.Compare(baseline); {
	case cmp < 0 && m.AllowDowngrade:
		d.EmergencyDowngrade = true // signed downgrade authorized; continue
	case cmp <= 0:
		d.Reason = ReasonDowngradeBlocked
		return d, fmt.Errorf("%w: target %s <= current %s", ErrDowngradeBlocked, target, baseline)
	}

	// 3. Min-supported floor: the current version must be high enough to apply this
	//    update (distinct from the server-side 426 protocol floor).
	if baseline.Compare(floor) < 0 {
		d.Reason = ReasonBelowFloor
		return d, fmt.Errorf("%w: current %s < min_supported %s", ErrBelowFloor, baseline, floor)
	}

	// 4. Kill-switch: only the SIGNED manifest may disable updates.
	if m.UpdateAllowed != nil && !*m.UpdateAllowed {
		d.Reason = ReasonUpdateNotAllowed
		return d, ErrUpdateNotAllowed
	}

	// 5. Artifact selection for this platform/arch.
	art, ok := m.ArtifactFor(platform, arch)
	if !ok {
		d.Reason = ReasonNoArtifact
		return d, fmt.Errorf("%w: %s/%s", ErrNoArtifact, platform, arch)
	}
	d.Artifact = art

	d.Proceed = true
	d.Reason = ReasonOK
	return d, nil
}

// Fetch retrieves the raw manifest bytes over the SPKI-pinned T12 client, bounding
// the response with the T13-C3 size cap. It returns ErrFetch (wrapping the cause,
// which may be httpclient.ErrPinMismatch / ErrResponseTooLarge) on any failure.
// maxBytes <= 0 uses DefaultManifestMaxBytes.
func Fetch(ctx context.Context, client *httpclient.Client, url string, maxBytes int64) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: nil client", ErrFetch)
	}
	if maxBytes <= 0 {
		maxBytes = DefaultManifestMaxBytes
	}
	rctx := httpclient.WithMaxResponseBytes(ctx, maxBytes)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err) // incl. ErrPinMismatch
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrFetch, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body) // bounded by the WithMaxResponseBytes cap
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetch, err) // incl. ErrResponseTooLarge
	}
	return body, nil
}

// Check fetches the manifest (Fetch) and evaluates it (Evaluate) — the convenience
// path for the orchestrator. On a fetch failure it returns a Decision with
// Reason=ReasonFetchFailed and the wrapped ErrFetch (fail closed). It does not
// retry, audit, or persist; those are the orchestrator's (T27).
func Check(ctx context.Context, client *httpclient.Client, url string, maxBytes int64, keys *trust.KeySet, baseline manifest.Version, platform, arch string) (*Decision, error) {
	raw, err := Fetch(ctx, client, url, maxBytes)
	if err != nil {
		return &Decision{CurrentVersion: baseline, Reason: ReasonFetchFailed}, err
	}
	return Evaluate(raw, keys, baseline, platform, arch)
}
