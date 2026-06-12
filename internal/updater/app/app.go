// Package app is the on-demand updater orchestrator (S1-T27). It is a PURE
// orchestrator: it sequences the already-verified components — manifestcheck
// (T24, decision), swap (T25, file primitives), healthgate (T26, health
// decision) — behind a crash-recoverable, persisted FSM. It implements NO new
// crypto, manifest-decision, hashing, swap, or health-decision logic.
//
// The updater runs on demand and exits (Technical Design §0.6 / AC-42): no
// persistent service, no self-update, no watchdog. State is persisted to a
// standalone updater_state.json (atomic write-temp-rename) before every
// side-effecting step so a crash mid-update resumes or rolls back to a consistent
// (binary, config) pair. Everything fails closed: any ambiguity before the swap
// aborts without touching the live binary; after the swap it rolls back.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/updater/healthgate"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/hashing"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// Audit event types emitted to the updater's own spool (frozen vocabulary, §5).
const (
	evStarted          = "update.started"
	evOffered          = "update.offered"
	evManifestVerified = "update.manifest_verified"
	evStaged           = "update.staged"
	evSwapped          = "update.swapped"
	evHealthOK         = "update.health_ok"
	evSucceeded        = "update.succeeded"
	evHashMismatch     = "update.hash_mismatch"
	evDowngradeBlocked = "update.downgrade_blocked"
	evSignatureInvalid = "update.signature_invalid"
	evRolledBack       = audit.EventUpdateRolledBack
	evFailed           = audit.EventUpdateFailed
)

// Outcome is the terminal result of a Check/Apply invocation (mapped to exit codes).
type Outcome string

const (
	OutcomeNoUpdate       Outcome = "no_update"
	OutcomeUpdated        Outcome = "updated"
	OutcomeRolledBack     Outcome = "rolled_back"
	OutcomeRollbackFailed Outcome = "rollback_failed"
	OutcomeRejected       Outcome = "rejected"
	OutcomeRecovered      Outcome = "recovered"
	OutcomeError          Outcome = "error"
)

// Typed errors. Match with errors.Is.
var (
	ErrConfig         = errors.New("updater: invalid configuration")
	ErrStaleManifest  = errors.New("updater: manifest released_at older than watermark (replay)")
	ErrQuarantined    = errors.New("updater: target version quarantined after a recent rollback")
	ErrAttemptCap     = errors.New("updater: attempt cap exceeded")
	ErrServiceControl = errors.New("updater: agent service control failed")
	ErrRollbackFailed = errors.New("updater: rollback restore failed (device may need manual recovery)")
	ErrResume         = errors.New("updater: cannot resume persisted state")
)

// DefaultMaxAttempts caps consecutive apply attempts for one target before it is
// quarantined (rollback-loop / crash-loop backstop).
const DefaultMaxAttempts = 2

// ---- collaborator interfaces (injected; prod impls in prod.go) ----------------

// Swapper is the T25 file-ops primitive set.
type Swapper interface {
	Stage(ctx context.Context, art manifest.Artifact, targetOS string) error
	Backup() (hashing.Digest, error)
	Swap() error
	Restore(wantBinDigest hashing.Digest) error
	Commit() error
}

// Gate is the T26 health decision.
type Gate interface {
	Wait(ctx context.Context, e healthgate.Expectation) healthgate.Result
}

// ServiceController stops/starts/queries the agent service by name (T18 helpers).
type ServiceController interface {
	Stop() error
	Start() error
	Running() (bool, error)
}

// ManifestChecker fetches+verifies+decides (T24). baseline is supplied by T27.
type ManifestChecker interface {
	Check(ctx context.Context, baseline manifest.Version) (*manifestcheck.Decision, error)
}

// MarkerWriter writes/clears the update_id marker + clears health.json (T26 contract).
type MarkerWriter interface {
	Write(updateID string) error
	Clear() error // remove marker AND health.json
}

// Auditor emits an updater audit event to the updater's OWN spool.
type Auditor interface {
	Emit(eventType, outcome string, detail map[string]any)
}

// Deps bundles the orchestrator's collaborators and parameters.
type Deps struct {
	Store        *StateStore
	Check        ManifestChecker
	Swap         Swapper
	Gate         Gate
	Service      ServiceController
	Marker       MarkerWriter
	Audit        Auditor
	BuildVersion manifest.Version // buildinfo version (anti-rollback floor)
	TargetOS     string           // "windows" | "linux" (PE/ELF gate + artifact match)
	Platform     string           // artifact platform (defaults to TargetOS)
	Arch         string           // artifact arch
	HealthWindow time.Duration    // gate window (0 -> healthgate.DefaultWindow)
	MaxAttempts  int              // 0 -> DefaultMaxAttempts
	Now          func() time.Time
	MintID       func() (string, error) // 0 -> crypto/rand 128-bit
	Log          *logging.Logger
}

// Updater is the on-demand FSM orchestrator.
type Updater struct {
	d Deps
}

// New validates deps and returns an Updater.
func New(d Deps) (*Updater, error) {
	if d.Store == nil || d.Check == nil || d.Swap == nil || d.Gate == nil ||
		d.Service == nil || d.Marker == nil || d.Audit == nil {
		return nil, fmt.Errorf("%w: nil collaborator", ErrConfig)
	}
	if d.TargetOS == "" {
		return nil, fmt.Errorf("%w: empty TargetOS", ErrConfig)
	}
	if d.Platform == "" {
		d.Platform = d.TargetOS
	}
	if d.HealthWindow <= 0 {
		d.HealthWindow = healthgate.DefaultWindow
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = DefaultMaxAttempts
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	if d.MintID == nil {
		d.MintID = mintUpdateID
	}
	return &Updater{d: d}, nil
}

// mintUpdateID returns a 128-bit CSPRNG hex nonce ("upd_<32 hex>").
func mintUpdateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "upd_" + hex.EncodeToString(b[:]), nil
}

// baseline = max(persisted current_version, buildinfo version).
func (u *Updater) baseline(st *State) manifest.Version {
	b := u.d.BuildVersion
	if c := st.current(); c.Compare(b) > 0 {
		return c
	}
	return b
}

// ---- Check (read-only decision) ----------------------------------------------

// Check fetches+verifies+evaluates the manifest and returns the decision WITHOUT
// any side effects (no staging, no swap, no state mutation). It still applies the
// T27-owned pre-filters (released_at freshness, quarantine) so the reported
// decision matches what Apply would do.
func (u *Updater) Check(ctx context.Context) (*manifestcheck.Decision, error) {
	st, err := u.d.Store.Load()
	if err != nil {
		return nil, err
	}
	dec, derr := u.d.Check.Check(ctx, u.baseline(st))
	if dec == nil {
		return nil, derr
	}
	if dec.Proceed {
		if _, ferr := u.freshnessErr(st, dec); ferr != nil {
			dec.Proceed = false
			dec.Reason = "stale_or_quarantined"
			return dec, ferr
		}
	}
	return dec, derr
}

// ---- Apply (the FSM) ----------------------------------------------------------

// Apply recovers any in-flight update first (crash recovery), then — only if idle —
// performs a fresh check + the full apply sequence, running to a terminal state and
// returning. It is the single entry point for the `apply` subcommand.
func (u *Updater) Apply(ctx context.Context) (Outcome, error) {
	st, err := u.d.Store.Load()
	if err != nil {
		return OutcomeError, err
	}
	if st.FSMState != StateIdle {
		return u.recover(ctx, st)
	}
	return u.freshApply(ctx, st)
}

// freshApply runs the decision + pre-filters, mints the update_id, and drives the
// forward FSM from MANIFEST_VERIFIED.
func (u *Updater) freshApply(ctx context.Context, st *State) (Outcome, error) {
	u.audit(evStarted, audit.OutcomeSuccess, nil)
	dec, derr := u.d.Check.Check(ctx, u.baseline(st))
	if dec == nil {
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "fetch"})
		return OutcomeError, derr
	}
	if !dec.Proceed {
		return u.handleRejection(dec), derr
	}
	candidate, ferr := u.freshnessErr(st, dec)
	if ferr != nil {
		u.audit(evFailed, audit.OutcomeDenied, map[string]any{
			"reason": rejectReason(ferr), "target": dec.TargetVersion.String(),
		})
		return OutcomeRejected, ferr
	}

	target := dec.TargetVersion.String()
	attempt := 1
	if st.AttemptTarget == target {
		attempt = st.Attempt + 1
	}
	if attempt > u.d.MaxAttempts {
		st.LastFailedTarget = target // quarantine after too many attempts
		st.TargetVersion, st.UpdateID, st.AttemptTarget, st.Attempt = "", "", "", 0
		st.PendingReleasedAt, st.FSMState = "", StateIdle
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err // fail closed: the quarantine MUST persist
		}
		u.audit(evFailed, audit.OutcomeDenied, map[string]any{
			"reason": "attempt_cap", "target": target,
		})
		return OutcomeRejected, fmt.Errorf("%w: %s after %d attempts", ErrAttemptCap, target, u.d.MaxAttempts)
	}

	updateID, err := u.d.MintID()
	if err != nil {
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "mint_id"})
		return OutcomeError, fmt.Errorf("%w: mint update_id: %v", ErrConfig, err)
	}

	st.FSMState = StateManifestVerified
	st.TargetVersion = target
	st.UpdateID = updateID
	st.AttemptTarget = target
	st.Attempt = attempt
	st.PendingReleasedAt = candidate
	st.Artifact = dec.Artifact
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err
	}
	u.audit(evOffered, audit.OutcomeSuccess, map[string]any{"target": target})
	u.audit(evManifestVerified, audit.OutcomeSuccess, map[string]any{
		"target": target, "key_id": keyID(dec),
	})
	return u.driveForward(ctx, st, dec)
}

// driveForward advances the forward FSM from st.FSMState to a terminal outcome.
// dec may be nil when resuming (the artifact is reloaded from persisted state).
func (u *Updater) driveForward(ctx context.Context, st *State, dec *manifestcheck.Decision) (Outcome, error) {
	switch st.FSMState {
	case StateManifestVerified:
		if err := u.d.Swap.Stage(ctx, st.Artifact, u.d.TargetOS); err != nil {
			ev, reason := evFailed, "stage"
			if errors.Is(err, verify.ErrHashMismatch) {
				ev, reason = evHashMismatch, "hash_mismatch"
			}
			u.audit(ev, audit.OutcomeFailure, map[string]any{"reason": reason, "target": st.TargetVersion})
			return u.abortPreSwap(st, err)
		}
		st.FSMState = StateStaged
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		u.audit(evStaged, audit.OutcomeSuccess, map[string]any{"target": st.TargetVersion})
		fallthrough

	case StateStaged:
		st.FSMState = StateStoppingAgent
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		if err := u.d.Service.Stop(); err != nil {
			u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "stop_agent"})
			return u.abortPreSwap(st, fmt.Errorf("%w: stop: %v", ErrServiceControl, err))
		}
		fallthrough

	case StateStoppingAgent:
		dg, err := u.d.Swap.Backup()
		if err != nil {
			u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "backup"})
			return u.abortPreSwap(st, err)
		}
		st.BackupDigest = dg.String()
		st.FSMState = StateBackedUp
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		fallthrough

	case StateBackedUp:
		st.FSMState = StateSwapping
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		if err := u.d.Swap.Swap(); err != nil {
			// Atomic rename failed → live binary is still the OLD one → abort (not a
			// rollback; nothing was replaced). Restart the old agent.
			u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "swap"})
			return u.abortPreSwap(st, err)
		}
		st.FSMState = StateStartingAgent
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		u.audit(evSwapped, audit.OutcomeSuccess, map[string]any{"target": st.TargetVersion})
		fallthrough

	case StateStartingAgent:
		return u.startAndGate(ctx, st)

	case StateHealthCheck:
		return u.runGate(ctx, st)

	default:
		return OutcomeError, fmt.Errorf("%w: unexpected forward state %q", ErrResume, st.FSMState)
	}
}

// startAndGate writes the marker FIRST (so the new agent can self-report — the
// frozen "marker before STARTING_AGENT" invariant), then persists HEALTH_CHECK with
// a fresh deadline, starts the new agent, and runs the gate.
func (u *Updater) startAndGate(ctx context.Context, st *State) (Outcome, error) {
	if err := u.d.Marker.Write(st.UpdateID); err != nil {
		// The new agent could not be told to self-report → it will never pass the
		// gate. We have not started it yet → abort (live binary is still the NEW one
		// only if we are past SWAPPING; rollback restores the prior pair).
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "marker"})
		return u.rollback(ctx, st, fmt.Errorf("marker write: %w", err))
	}
	deadline := u.d.Now().Add(u.d.HealthWindow)
	st.HealthDeadline = deadline.Format(time.RFC3339)
	st.FSMState = StateHealthCheck
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err
	}
	if err := u.d.Service.Start(); err != nil {
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": "start_agent"})
		return u.rollback(ctx, st, fmt.Errorf("%w: start: %v", ErrServiceControl, err))
	}
	return u.runGate(ctx, st)
}

// runGate evaluates the health gate (using the persisted deadline so a resumed
// HEALTH_CHECK is deterministic) and commits or rolls back.
func (u *Updater) runGate(ctx context.Context, st *State) (Outcome, error) {
	deadline, err := time.Parse(time.RFC3339, st.HealthDeadline)
	if err != nil {
		return u.rollback(ctx, st, fmt.Errorf("%w: bad health_deadline", ErrResume))
	}
	res := u.d.Gate.Wait(ctx, healthgate.Expectation{
		UpdateID:  st.UpdateID,
		GateStart: deadline.Add(-u.d.HealthWindow),
		Deadline:  deadline,
	})
	if res.Healthy {
		return u.commit(st)
	}
	return u.rollback(ctx, st, fmt.Errorf("health gate: %s/%s", res.Reason, res.Detail))
}

// commit is the success commit point: raise current_version + released_at watermark
// ATOMICALLY and go IDLE BEFORE deleting the backup, so a crash before the state
// write re-gates (the .bak is still intact). Then best-effort cleanup.
func (u *Updater) commit(st *State) (Outcome, error) {
	target := st.TargetVersion
	st.CurrentVersion = target
	if st.PendingReleasedAt != "" {
		st.LastSeenReleasedAt = st.PendingReleasedAt
	}
	st.FSMState = StateIdle
	st.TargetVersion, st.UpdateID, st.HealthDeadline, st.BackupDigest = "", "", "", ""
	st.PendingReleasedAt, st.AttemptTarget, st.Attempt = "", "", 0
	st.LastFailedTarget = "" // success clears the quarantine
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err // .bak intact → next run re-gates
	}
	_ = u.d.Swap.Commit() // best-effort: delete .bak (idempotent)
	u.clearMarker()
	u.audit(evHealthOK, audit.OutcomeSuccess, map[string]any{"target": target})
	u.audit(evSucceeded, audit.OutcomeSuccess, map[string]any{"version": target})
	return OutcomeUpdated, nil
}

// rollback restores the prior (binary, config) pair, restarts, quarantines the
// failed target, and goes IDLE. If Restore fails the state stays ROLLING_BACK so
// the next invocation retries; current_version is never changed by a rollback.
func (u *Updater) rollback(ctx context.Context, st *State, cause error) (Outcome, error) {
	st.FSMState = StateRollingBack
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err
	}
	_ = u.d.Service.Stop() // best-effort: stop the failed/new agent

	dg, perr := hashing.ParseDigest(st.BackupDigest)
	if perr != nil {
		u.audit(evRolledBack, audit.OutcomeFailure, map[string]any{
			"reason": "bad_backup_digest", "target": st.TargetVersion,
		})
		return OutcomeRollbackFailed, fmt.Errorf("%w: %v", ErrRollbackFailed, perr)
	}
	if err := u.d.Swap.Restore(dg); err != nil {
		// Stay ROLLING_BACK so the next invocation retries the restore.
		u.audit(evRolledBack, audit.OutcomeFailure, map[string]any{
			"reason": "restore_failed", "target": st.TargetVersion, "err": err.Error(),
		})
		return OutcomeRollbackFailed, fmt.Errorf("%w: %v (cause: %v)", ErrRollbackFailed, err, cause)
	}
	_ = u.d.Service.Start() // best-effort: bring the old agent back

	failed := st.TargetVersion
	st.LastFailedTarget = failed // quarantine the just-failed target (loop breaker)
	st.FSMState = StateIdle
	st.TargetVersion, st.UpdateID, st.HealthDeadline, st.BackupDigest, st.PendingReleasedAt = "", "", "", "", ""
	st.AttemptTarget, st.Attempt = "", 0 // quarantine (not the counter) now breaks the loop
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err
	}
	u.clearMarker()
	u.audit(evRolledBack, audit.OutcomeSuccess, map[string]any{
		"target": failed, "cause": cause.Error(),
	})
	return OutcomeRolledBack, nil
}

// abortPreSwap handles a failure BEFORE the swap (the live binary is untouched):
// ensure the agent is running again, clear the in-flight fields, and return to IDLE.
// It KEEPS AttemptTarget + Attempt (the crash-loop counter) and the watermark/
// quarantine; it clears TargetVersion + PendingReleasedAt so IDLE has no in-flight
// state, and clears any marker that an earlier (resumed) step may have written.
func (u *Updater) abortPreSwap(st *State, cause error) (Outcome, error) {
	_ = u.d.Service.Start() // idempotent: ensure the (unchanged) agent is up
	u.clearMarker()
	st.FSMState = StateIdle
	st.TargetVersion, st.UpdateID, st.HealthDeadline = "", "", ""
	st.BackupDigest, st.PendingReleasedAt = "", ""
	if err := u.d.Store.Save(st); err != nil {
		return OutcomeError, err
	}
	return OutcomeError, cause
}

// ---- crash recovery -----------------------------------------------------------

// recover dispatches a non-idle persisted state per the resume table: pre-swap
// states are abort-safe; post-swap states resume forward to the gate or rollback;
// ROLLING_BACK retries the restore.
func (u *Updater) recover(ctx context.Context, st *State) (Outcome, error) {
	u.audit(evStarted, audit.OutcomeSuccess, map[string]any{"resume": st.FSMState})
	switch {
	case isPreSwap(st.FSMState):
		// Live binary provably untouched → safe to abort. A failed state save must
		// NOT be hidden behind a success outcome (fail closed).
		from := st.FSMState
		if _, err := u.abortPreSwap(st, nil); err != nil {
			return OutcomeError, err
		}
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{
			"reason": "recovered_pre_swap", "from": from,
		})
		return OutcomeRecovered, nil

	case st.FSMState == StateSwapping:
		// Boundary: the swap MAY have completed. Attempt it once — ErrNotStaged
		// means it already happened (the .new was consumed). Either way the new
		// binary is then live; advance. A real swap error means the live binary is
		// still old → abort. This is the ONLY place Swap is (re)attempted on resume.
		if err := u.d.Swap.Swap(); err != nil && !errors.Is(err, swap.ErrNotStaged) {
			return u.abortPreSwap(st, fmt.Errorf("%w: resume swap: %v", ErrResume, err))
		}
		st.FSMState = StateStartingAgent
		if err := u.d.Store.Save(st); err != nil {
			return OutcomeError, err
		}
		return u.startAndGate(ctx, st)

	case st.FSMState == StateStartingAgent:
		return u.startAndGate(ctx, st)

	case st.FSMState == StateHealthCheck:
		// Re-establish the marker (in case the crash lost it) and a FRESH gate
		// window — a clock skip forward or a long crash must not unfairly fail a
		// healthy new agent. Service.Start is idempotent if it is already running.
		return u.startAndGate(ctx, st)

	case st.FSMState == StateRollingBack:
		return u.rollback(ctx, st, errors.New("resumed rollback"))

	default:
		return OutcomeError, fmt.Errorf("%w: %q", ErrResume, st.FSMState)
	}
}

// ---- pre-filters --------------------------------------------------------------

// freshnessErr enforces released_at monotonicity (FI-T24-1) and the quarantine of a
// just-failed target, PURELY (no mutation — so Check is side-effect-free). It returns
// the candidate released_at the caller promotes to PendingReleasedAt. Fails closed.
//
// "First update" is gated on CurrentVersion=="" (a one-time condition), NOT on an
// empty watermark: otherwise a first update whose released_at is absent would leave
// the watermark empty and make the first-update bypass re-triggerable forever.
func (u *Updater) freshnessErr(st *State, dec *manifestcheck.Decision) (string, error) {
	rel := ""
	if dec.Manifest != nil {
		rel = dec.Manifest.ReleasedAt
	}
	switch {
	case st.CurrentVersion == "":
		// First update ever on this device: released_at may be absent.
	case rel == "":
		return "", fmt.Errorf("%w: released_at required after the first update", ErrStaleManifest)
	case st.LastSeenReleasedAt == "":
		// Already updated once (first had no released_at): accept any present value
		// and establish the watermark now (no lower bound to compare against).
		if _, err := time.Parse(time.RFC3339, rel); err != nil {
			return "", fmt.Errorf("%w: unparseable released_at", ErrStaleManifest)
		}
	default:
		got, err1 := time.Parse(time.RFC3339, rel)
		want, err2 := time.Parse(time.RFC3339, st.LastSeenReleasedAt)
		if err1 != nil || err2 != nil {
			return "", fmt.Errorf("%w: unparseable released_at", ErrStaleManifest)
		}
		if got.Before(want) {
			return "", fmt.Errorf("%w: %s < %s", ErrStaleManifest, rel, st.LastSeenReleasedAt)
		}
	}
	// Quarantine: refuse a target that is not strictly newer than the last failure.
	if st.LastFailedTarget != "" {
		failed, err := manifest.ParseVersion(st.LastFailedTarget)
		if err == nil && dec.TargetVersion.Compare(failed) <= 0 {
			return "", fmt.Errorf("%w: %s <= last_failed %s", ErrQuarantined, dec.TargetVersion.String(), st.LastFailedTarget)
		}
	}
	return rel, nil
}

// handleRejection maps a non-proceed decision to the right audit event + outcome.
// target == current is the benign "already up to date" no-op (no audit noise).
func (u *Updater) handleRejection(dec *manifestcheck.Decision) Outcome {
	switch dec.Reason {
	case manifestcheck.ReasonDowngradeBlocked:
		if dec.TargetVersion.Compare(dec.CurrentVersion) == 0 {
			return OutcomeNoUpdate // up to date
		}
		u.audit(evDowngradeBlocked, audit.OutcomeDenied, map[string]any{
			"target": dec.TargetVersion.String(), "current": dec.CurrentVersion.String(),
		})
		return OutcomeRejected
	case manifestcheck.ReasonManifestRejected:
		u.audit(evSignatureInvalid, audit.OutcomeDenied, map[string]any{"reason": dec.Reason})
		return OutcomeRejected
	case manifestcheck.ReasonFetchFailed:
		// Transient (network) — not a permanent rejection; retry on the next run.
		u.audit(evFailed, audit.OutcomeFailure, map[string]any{"reason": dec.Reason})
		return OutcomeError
	default:
		u.audit(evFailed, audit.OutcomeDenied, map[string]any{"reason": dec.Reason})
		return OutcomeRejected
	}
}

// ---- helpers ------------------------------------------------------------------

func (u *Updater) audit(eventType, outcome string, detail map[string]any) {
	u.d.Audit.Emit(eventType, outcome, detail)
}

// clearMarker removes the marker + health.json after a terminal step. It is
// best-effort (the update already happened) but the error is LOGGED, never silently
// dropped — a leftover marker is otherwise only defended by the gate's update_id.
func (u *Updater) clearMarker() {
	if err := u.d.Marker.Clear(); err != nil && u.d.Log != nil {
		u.d.Log.Warn("updater.marker_clear_failed").Str("err", err.Error()).Msg("")
	}
}

func keyID(dec *manifestcheck.Decision) string {
	if dec.Manifest != nil {
		return dec.Manifest.KeyID
	}
	return ""
}

func rejectReason(err error) string {
	switch {
	case errors.Is(err, ErrStaleManifest):
		return "stale_manifest"
	case errors.Is(err, ErrQuarantined):
		return "quarantined"
	default:
		return "rejected"
	}
}
