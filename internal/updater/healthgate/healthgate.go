// Package healthgate is the updater's post-update health GATE (S1-T26): it waits
// for the new agent to prove it is healthy — the agent service is RUNNING AND the
// agent has written a fresh, valid health record (internal/health) for THIS update
// — within a bounded window (90s), and returns a typed pass/fail decision.
//
// SCOPE (frozen): a PURE DECISION layer. It does NOT call swap.Restore, start/stop
// the service, persist updater_state.json / current_version, emit audit, or report
// to the SaaS — those are the orchestrator's (T27). It only DECIDES health. The
// service-RUNNING check is injected (a func, so this package does not import the
// service package). Every ambiguity at the deadline fails closed → Unhealthy.
//
// Freshness: the gate requires health.UpdateID == the update_id the updater
// generated for THIS swap, and health.WrittenAt within the active window — so a
// stale health.json from a PRIOR update (different update_id) can never pass.
package healthgate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/health"
)

// DefaultWindow is the bounded health-gate window (UPD-4).
const DefaultWindow = 90 * time.Second

// defaultPoll is how often the gate re-checks the two conditions.
const defaultPoll = 2 * time.Second

// Decision reasons (the orchestrator maps these to update.* audit events).
const (
	ReasonOK                = "ok"
	ReasonTimeout           = "timeout"             // window elapsed without both conditions
	ReasonServiceNotRunning = "service_not_running" // last-seen sub-reason at timeout
	ReasonNoHealth          = "no_health"           // no health record by the deadline
	ReasonStaleHealth       = "stale_health"        // wrong update_id / outside the window
	ReasonHealthFailed      = "health_failed"       // agent self-reported result=failed
	ReasonCanceled          = "canceled"            // context canceled before a decision
)

// ErrServiceQuery wraps a failure to query the service status (non-fatal mid-poll;
// surfaced as the timeout sub-reason if it persists to the deadline).
var ErrServiceQuery = errors.New("healthgate: service status query failed")

// Result is the gate's typed decision. Healthy == (Reason == ReasonOK).
type Result struct {
	Healthy bool
	Reason  string
	// Detail carries the last observed sub-reason / error string for diagnostics.
	Detail string
}

// Expectation binds the gate to THIS update attempt.
type Expectation struct {
	// UpdateID the agent must echo in its health record (the updater's CSPRNG nonce).
	UpdateID string
	// GateStart is when the window opened (the health record's written_at must be
	// at or after this — same-host clock, so no skew).
	GateStart time.Time
	// Deadline is the hard end of the window (GateStart + 90s).
	Deadline time.Time
}

// Gate evaluates post-update health by polling the agent's state directory and an
// injected service-RUNNING check.
type Gate struct {
	stateDir string
	running  func() (bool, error) // injected: true iff the agent service is RUNNING
	poll     time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
}

// Option customizes a Gate (mainly for tests).
type Option func(*Gate)

// WithPoll overrides the poll interval.
func WithPoll(d time.Duration) Option {
	return func(g *Gate) {
		if d > 0 {
			g.poll = d
		}
	}
}

// WithClock overrides the clock (tests).
func WithClock(now func() time.Time) Option {
	return func(g *Gate) {
		if now != nil {
			g.now = now
		}
	}
}

// WithSleeper overrides the inter-poll sleep (tests).
func WithSleeper(f func(context.Context, time.Duration) error) Option {
	return func(g *Gate) {
		if f != nil {
			g.sleep = f
		}
	}
}

// New builds a Gate over stateDir, using running() to check the service RUNNING
// condition. running must not be nil.
func New(stateDir string, running func() (bool, error), opts ...Option) (*Gate, error) {
	if stateDir == "" {
		return nil, errors.New("healthgate: empty state dir")
	}
	if running == nil {
		return nil, errors.New("healthgate: nil service-running check")
	}
	g := &Gate{stateDir: stateDir, running: running, poll: defaultPoll, now: time.Now, sleep: ctxSleep}
	for _, o := range opts {
		o(g)
	}
	return g, nil
}

// Wait polls until BOTH conditions hold (Healthy), the agent self-reports failure
// (Unhealthy: health_failed), the deadline elapses (Unhealthy: timeout + sub-reason),
// or ctx is canceled. It always returns a non-nil Result; Healthy ⟺ Reason==ok.
func (g *Gate) Wait(ctx context.Context, e Expectation) Result {
	lastReason := ReasonNoHealth
	for {
		if ctx.Err() != nil {
			return Result{Reason: ReasonCanceled, Detail: ctx.Err().Error()}
		}
		running, rec, ok, reason, detail := g.evaluate(e)
		if ok {
			return Result{Healthy: true, Reason: ReasonOK}
		}
		if reason == ReasonHealthFailed {
			return Result{Reason: ReasonHealthFailed, Detail: detail} // definitive failure -> roll back now
		}
		lastReason = reason
		_ = running
		_ = rec
		if !g.now().Before(e.Deadline) {
			return Result{Reason: ReasonTimeout, Detail: lastReason}
		}
		// Sleep until the next poll, but never past the deadline (injected clock).
		wait := g.poll
		if rem := e.Deadline.Sub(g.now()); rem < wait {
			wait = rem
		}
		if wait <= 0 {
			// The deadline arrived (possibly the clock advanced since the check
			// above) — stop now rather than spin on a non-positive sleep.
			return Result{Reason: ReasonTimeout, Detail: lastReason}
		}
		if err := g.sleep(ctx, wait); err != nil {
			return Result{Reason: ReasonCanceled, Detail: err.Error()}
		}
	}
}

// evaluate performs one check of the two conditions, returning whether the gate
// passes, plus the not-yet sub-reason. A wrong-update_id / out-of-window record is
// treated as "not yet" (keep polling for a fresh one); a result==failed record with
// the matching update_id is a definitive failure.
func (g *Gate) evaluate(e Expectation) (running bool, rec health.Record, pass bool, reason, detail string) {
	running, rerr := g.running()
	rec, herr := health.ReadHealth(g.stateDir)

	if herr != nil {
		if errors.Is(herr, health.ErrAbsent) {
			return running, rec, false, ReasonNoHealth, ""
		}
		return running, rec, false, ReasonStaleHealth, herr.Error() // malformed/invalid -> not yet
	}
	if rec.UpdateID != e.UpdateID {
		return running, rec, false, ReasonStaleHealth, "update_id mismatch"
	}
	if !g.freshWrittenAt(rec.WrittenAt, e) {
		return running, rec, false, ReasonStaleHealth, "written_at outside window"
	}
	if rec.Result == health.ResultFailed {
		return running, rec, false, ReasonHealthFailed, "agent reported failure"
	}
	// result == ok with a matching, fresh update_id. Both conditions required.
	if rerr != nil {
		return running, rec, false, ReasonServiceNotRunning, fmt.Sprintf("%v: %v", ErrServiceQuery, rerr)
	}
	if !running {
		return running, rec, false, ReasonServiceNotRunning, ""
	}
	return running, rec, true, ReasonOK, ""
}

// freshWrittenAt requires the record's written_at to parse and be within the active
// window [GateStart, Deadline]. Agent and updater share the host clock (no skew); the
// lower bound is floored to the second because written_at is RFC3339 (second
// precision) and could otherwise round just below a sub-second GateStart.
func (g *Gate) freshWrittenAt(s string, e Expectation) bool {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return false
	}
	return !t.Before(e.GateStart.Truncate(time.Second)) && !t.After(e.Deadline)
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
