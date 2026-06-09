// Package tasks implements the agent task-poll use-case (S1-T16): a thin,
// server-directed, jittered, low-retry poll loop that runs the Sprint-1 `noop`
// ack/status round-trip and recognizes-but-does-not-execute the reserved task
// types.
//
// It is a single-purpose poll loop — NOT a general scheduler. There is no
// backup/restore/update execution, no persistent task cursor, and no local lease
// store (the lease is server-side; Redis, Sprint 2). Frozen design (senior
// review, 2026-06-09):
//   - Cadence is SERVER-DIRECTED: TasksResponse.NextPollSeconds clamped to
//     [PollFloorSeconds=60, PollCeilingSeconds=86400] (defends against a bad/hostile
//     value), with config.task_poll_interval_seconds as the pre-first-response
//     fallback and mandatory ±poll_jitter_pct jitter (default 20%).
//   - work_available pokes the loop to poll SOON, but within a small JITTERED
//     window (never literally instantly), to avoid a server-broadcast herd.
//   - Sprint-1 scope: empty list + `noop` only (Ack -> ReportStatus succeeded,
//     each with a DETERMINISTIC Idempotency-Key per task_id+operation); an
//     in-memory seen-task set prevents duplicate processing within a process.
//     `update_check`/`config_refresh` are recognized-but-not-executed (logged, NOT
//     acked); an unknown type is logged + skipped. Stateless across restarts.
//   - Errors: 401 -> audit auth.failure + STOP (re-enroll); 426 -> STOP (updater,
//     ADR-004); transient (5xx/429/network) -> exponential backoff + jitter
//     (circuit-breaker) and keep looping.
//
// Routine poll/ack/status are LOGGED, not audited: the frozen T09 vocabulary has
// no task event, task-lifecycle audit is a server-side concern, and auditing
// fleet-scale poll telemetry would swamp the chain. The only task-path audit is
// auth.failure on a 401. The MaxRetries=1 low-retry control lives in the T12
// transport config (composition root), not this package. T13-C3 (response-size
// cap) remains a forward item and is NOT implemented here.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// Frozen cadence constants (senior review, 2026-06-09).
const (
	PollFloorSeconds   = 60    // minimum effective poll interval
	PollCeilingSeconds = 86400 // maximum effective poll interval (schema max)
	DefaultJitterPct   = 20    // default jitter when the server provides none
	maxJitterPct       = 50    // matches the OpenAPI poll_jitter_pct ceiling

	// workAvailableWindowSeconds is the small base window for a work_available /
	// poke-triggered poll (jittered, never instant).
	workAvailableWindowSeconds = 5

	// supportedTaskSchemaVersion is the highest task envelope schema this agent
	// fully understands. A higher version on a known, payload-free type (noop) is
	// handled best-effort (unknown fields ignored, additionalProperties).
	supportedTaskSchemaVersion = 1

	// maxConsecutiveWorkAvailable bounds back-to-back work_available "poll soon"
	// windows so the loop cannot be pinned below the server cadence (herd/hot-poll
	// defense); after the cap it falls back to the clamped server interval.
	maxConsecutiveWorkAvailable = 10

	// idempotencyNamespacePrefix scopes the deterministic ack/status keys.
	idempotencyNamespacePrefix = "beyz:task:"
)

// Typed errors. Match with errors.Is.
var (
	// ErrNotEnrolled means the device is not fully enrolled (device_id +
	// certificate + session_token must all be present); the loop must not start.
	ErrNotEnrolled = errors.New("tasks: device not enrolled")
	// ErrUnauthorized is a 401 (token invalid/revoked). TERMINAL — the loop stops
	// and the caller routes to re-enrollment.
	ErrUnauthorized = errors.New("tasks: session token rejected (401)")
	// ErrUpgradeRequired is a 426. TERMINAL — the loop stops and the caller routes
	// to the updater (ADR-004).
	ErrUpgradeRequired = errors.New("tasks: protocol upgrade required (426)")
	// ErrPollFailed is a transient failure (transport/5xx/429 after low retry). The
	// loop backs off and continues.
	ErrPollFailed = errors.New("tasks: transient poll failure")
)

// TaskClient is the typed transport (S1-T13). Satisfied by *saasclient.Client.
type TaskClient interface {
	PollTasks(ctx context.Context, deviceID string) (*proto.TasksResponse, error)
	AckTask(ctx context.Context, deviceID, taskID string, body proto.TaskAckRequest, opts ...saasclient.RequestOption) (*proto.TaskAckResponse, error)
	ReportTaskStatus(ctx context.Context, deviceID, taskID string, body proto.TaskStatusRequest, opts ...saasclient.RequestOption) (*proto.TaskStatusResponse, error)
	// SetSessionToken seeds the in-memory bearer cache from durable state at startup.
	SetSessionToken(token string)
}

// StateReader reads the enrolled credential. Satisfied by *state.Store.
type StateReader interface {
	Get(key string) ([]byte, error)
	GetSecret(key string) ([]byte, error)
}

// AuditEmitter records hash-chained audit events (S1-T09). Satisfied by *audit.Emitter.
type AuditEmitter interface {
	Emit(ev audit.Event) (audit.Record, error)
}

// Deps bundles the task-poll collaborators and inputs.
type Deps struct {
	Config *config.Config
	Client TaskClient
	State  StateReader
	Audit  AuditEmitter
	// Log is optional (nil disables operational logging). It never receives secrets.
	Log *logging.Logger
	// Poke is an optional channel; a receive wakes the loop to poll soon (within a
	// jittered window). Fed by the heartbeat's work_available via the composition root.
	Poke <-chan struct{}
	// Now/Wait/Rand are injectable for tests; nil uses production defaults. Wait
	// blocks for d or until a poke / ctx, returning poked=true if a poke woke it.
	Now  func() time.Time
	Wait func(ctx context.Context, d time.Duration) (poked bool, err error)
	Rand func() float64 // jitter source in [0,1)
}

// Stats is the observability snapshot.
type Stats struct {
	LastPollSuccess     time.Time
	ConsecutiveFailures int
	LastTaskCount       int
}

// PollResult is the outcome of a single poll.
type PollResult struct {
	NextInterval  time.Duration
	TaskCount     int
	Processed     int // noop tasks acked + reported
	Skipped       int // recognized-not-executed / unknown / already-seen
	Failed        int // tasks whose ack/report failed transiently (left for redelivery)
	WorkAvailable bool
}

// Poller is the task-poll use-case.
type Poller struct {
	deps     Deps
	deviceID string

	mu                  sync.Mutex
	seen                map[string]struct{} // in-memory seen-task set (handled this process)
	lastPollSuccess     time.Time
	consecutiveFailures int
	lastTaskCount       int
}

type dispatchOutcome int

const (
	outcomeProcessed dispatchOutcome = iota
	outcomeSkipped
	outcomeFailed // ack/report failed transiently; task left unseen for redelivery
)

// New validates dependencies, enforces the enrolled predicate, seeds the client's
// in-memory token from durable state, and returns a Poller. It returns
// ErrNotEnrolled unless device_id, agent_certificate_pem, and agent_session_token
// are all present.
func New(deps Deps) (*Poller, error) {
	switch {
	case deps.Config == nil:
		return nil, errors.New("tasks: nil config")
	case deps.Client == nil:
		return nil, errors.New("tasks: nil client")
	case deps.State == nil:
		return nil, errors.New("tasks: nil state")
	case deps.Audit == nil:
		return nil, errors.New("tasks: nil audit")
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.Rand == nil {
		deps.Rand = rand.Float64 // per-process auto-seeded; de-synchronizes the fleet
	}

	deviceID, err := nonEmpty(deps.State.Get(state.KeyDeviceID))
	if err != nil {
		return nil, fmt.Errorf("%w: device_id: %v", ErrNotEnrolled, err)
	}
	if _, err := nonEmpty(deps.State.Get(state.KeyCertificate)); err != nil {
		return nil, fmt.Errorf("%w: certificate: %v", ErrNotEnrolled, err)
	}
	token, err := nonEmpty(deps.State.GetSecret(state.SecretSessionToken))
	if err != nil {
		return nil, fmt.Errorf("%w: session_token: %v", ErrNotEnrolled, err)
	}
	deps.Client.SetSessionToken(string(token))

	p := &Poller{deps: deps, deviceID: string(deviceID), seen: make(map[string]struct{})}
	return p, nil
}

// PollOnce performs one poll: it fetches the task list, dispatches each task, and
// returns the next (clamped, jittered) interval. A terminal error (401/426) from
// the poll or any task's ack/report aborts and is returned with the typed
// sentinel; transient task failures are logged and left for redelivery.
func (p *Poller) PollOnce(ctx context.Context) (*PollResult, error) {
	resp, err := p.deps.Client.PollTasks(ctx, p.deviceID)
	if err != nil {
		return nil, p.handlePollError(err)
	}
	if resp == nil {
		p.onFailure()
		p.logWarn("tasks.poll_failed", "reason", "empty_response")
		return nil, fmt.Errorf("%w: empty response", ErrPollFailed)
	}

	processed, skipped, failed := 0, 0, 0
	for i := range resp.Tasks {
		outcome, terr := p.dispatch(ctx, resp.Tasks[i])
		if terr != nil {
			return nil, terr // terminal (401/426) — abort the whole poll
		}
		switch outcome {
		case outcomeProcessed:
			processed++
		case outcomeFailed:
			failed++
		default:
			skipped++
		}
	}

	next := p.nextInterval(resp.NextPollSeconds, resp.PollJitterPct)
	// A transient ack/report failure on any task makes this a DEGRADED poll: count
	// it so backoff engages and Stats reflects the failing endpoint, rather than
	// hammering it at full cadence while reporting healthy.
	if failed > 0 {
		p.onPartialFailure(len(resp.Tasks))
	} else {
		p.onSuccess(len(resp.Tasks))
	}
	p.logPollOK(len(resp.Tasks), processed, skipped, failed, next, resp.WorkAvailable)
	return &PollResult{
		NextInterval:  next,
		TaskCount:     len(resp.Tasks),
		Processed:     processed,
		Skipped:       skipped,
		Failed:        failed,
		WorkAvailable: resp.WorkAvailable,
	}, nil
}

// Run is the single-purpose poll loop: bounded jittered startup delay, then poll
// on the server-directed cadence, polling soon (jittered window) on a poke /
// work_available, backing off with jitter on transient failures, and stopping on
// a terminal error (401/426) or context cancellation.
func (p *Poller) Run(ctx context.Context) error {
	interval := p.startupDelay()
	workStreak := 0 // consecutive work_available "poll soon" windows (bounded)
	for {
		poked, err := p.wait(ctx, interval)
		if err != nil {
			return ctx.Err()
		}
		if poked {
			// Do not poll instantly: poll within a small jittered window.
			interval = p.workAvailableInterval()
			continue
		}
		res, err := p.PollOnce(ctx)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUpgradeRequired) {
				return err
			}
			workStreak = 0
			interval = p.backoffInterval(p.failureCount())
			continue
		}
		switch {
		case res.Failed > 0:
			// Degraded poll (a task ack/report failed transiently): back off instead
			// of hammering the failing endpoint at full cadence.
			workStreak = 0
			interval = p.backoffInterval(p.failureCount())
		case res.WorkAvailable && res.Processed > 0 && workStreak < maxConsecutiveWorkAvailable:
			// Honor "poll soon" only when progress was actually made and within a
			// bounded streak, so an unacked/no-progress work_available cannot pin the
			// loop at the ~5s window below the clamped server cadence.
			workStreak++
			interval = p.workAvailableInterval()
		default:
			workStreak = 0
			interval = res.NextInterval
		}
	}
}

// Stats returns the current observability snapshot.
func (p *Poller) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Stats{
		LastPollSuccess:     p.lastPollSuccess,
		ConsecutiveFailures: p.consecutiveFailures,
		LastTaskCount:       p.lastTaskCount,
	}
}

// ---- dispatch ---------------------------------------------------------------

// dispatch handles one task. It returns (outcome, terminalError). A terminal
// error (401/426 during ack/report) aborts the poll; a transient task error is
// logged and the task is left unseen for redelivery.
func (p *Poller) dispatch(ctx context.Context, t proto.TaskEnvelope) (dispatchOutcome, error) {
	if p.alreadySeen(t.TaskId) {
		return outcomeSkipped, nil // in-memory dedup: already handled this process
	}
	switch t.Type {
	case proto.Noop:
		if t.SchemaVersion > supportedTaskSchemaVersion {
			// noop carries no payload; a higher schema is forward-compat-safe.
			p.logWarn("tasks.noop_higher_schema", "task_id", t.TaskId)
		}
		if err := p.runNoop(ctx, t); err != nil {
			if terr := p.terminalError(err); terr != nil {
				return outcomeFailed, terr // abort poll
			}
			p.logWarn("tasks.noop_failed", "task_id", t.TaskId) // transient: redelivered later
			return outcomeFailed, nil                           // degraded; not marked seen
		}
		p.markSeen(t.TaskId)
		return outcomeProcessed, nil
	case proto.UpdateCheck, proto.ConfigRefresh:
		// Recognized but NOT executed in Sprint 1 — and NOT acked (acking implies
		// the agent will handle it). Mark seen so it is logged once per process.
		p.logInfo("tasks.recognized_not_executed", "task_id", t.TaskId, "type", string(t.Type))
		p.markSeen(t.TaskId)
		return outcomeSkipped, nil
	default:
		// Unknown type (forward-compat): log + skip, never execute, never ack.
		p.logWarn("tasks.unknown_type", "task_id", t.TaskId, "type", string(t.Type))
		p.markSeen(t.TaskId)
		return outcomeSkipped, nil
	}
}

// runNoop runs the noop round-trip: Ack then ReportStatus(succeeded), each with a
// deterministic Idempotency-Key so a retry (in-process or cross-restart) dedupes.
func (p *Poller) runNoop(ctx context.Context, t proto.TaskEnvelope) error {
	ackKey := idempotencyKey(t.TaskId, "ack")
	if _, err := p.deps.Client.AckTask(ctx, p.deviceID, t.TaskId, proto.TaskAckRequest{}, saasclient.WithIdempotencyKey(ackKey)); err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	statusKey := idempotencyKey(t.TaskId, "status")
	body := proto.TaskStatusRequest{Status: proto.Succeeded}
	if _, err := p.deps.Client.ReportTaskStatus(ctx, p.deviceID, t.TaskId, body, saasclient.WithIdempotencyKey(statusKey)); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return nil
}

// idempotencyKey is a deterministic UUIDv5 over the task id and operation, so the
// same logical ack/status always carries the same key.
func idempotencyKey(taskID, op string) proto.IdempotencyKey {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(idempotencyNamespacePrefix+taskID+":"+op))
}

// ---- error handling ---------------------------------------------------------

// handlePollError maps a poll-level transport error: terminal (401/426) -> typed
// terminal (auditing a 401); otherwise transient -> ErrPollFailed.
func (p *Poller) handlePollError(err error) error {
	if terr := p.terminalError(err); terr != nil {
		return terr
	}
	p.onFailure()
	p.logWarn("tasks.poll_failed", "reason", "transport")
	return fmt.Errorf("%w: %v", ErrPollFailed, err)
}

// terminalError returns a typed terminal error (auditing a 401) when err is a 401
// or 426, else nil. Used by both the poll-level and task-level paths.
func (p *Poller) terminalError(err error) error {
	switch {
	case errors.Is(err, saasclient.ErrUnauthorized):
		p.emitAuthFailure()
		p.logWarn("tasks.auth_failure", "reason", "task_401")
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	case errors.Is(err, saasclient.ErrUpgradeRequired):
		p.logWarn("tasks.upgrade_required", "reason", "task_426")
		return fmt.Errorf("%w: %v", ErrUpgradeRequired, err)
	}
	return nil
}

func (p *Poller) emitAuthFailure() {
	_, _ = p.deps.Audit.Emit(audit.Event{
		EventType: audit.EventAuthFailure,
		Category:  audit.CategoryAuth,
		Severity:  audit.SeverityWarn,
		Outcome:   audit.OutcomeDenied,
		Actor:     "system",
		Detail:    map[string]any{"reason": "task_poll_401", "device_id": p.deviceID},
	})
}

// ---- cadence ----------------------------------------------------------------

func (p *Poller) nextInterval(serverSeconds int, pollJitterPct *int) time.Duration {
	base := clampSeconds(serverSeconds)
	jit := DefaultJitterPct
	if pollJitterPct != nil {
		jit = clampJitter(*pollJitterPct)
	}
	return jitter(base, jit, p.deps.Rand())
}

// workAvailableInterval is the small jittered window before a poke/work_available
// poll (never instant), spreading a server-broadcast herd.
func (p *Poller) workAvailableInterval() time.Duration {
	return jitter(workAvailableWindowSeconds, DefaultJitterPct, p.deps.Rand())
}

// startupDelay spreads a fleet-restart herd over the floor's jitter window.
func (p *Poller) startupDelay() time.Duration {
	base := clampSeconds(p.deps.Config.Heartbeat.TaskPollIntervalSeconds)
	maxDelay := float64(base) * float64(DefaultJitterPct) / 100.0
	return time.Duration(p.deps.Rand() * maxDelay * float64(time.Second))
}

// backoffInterval is an exponential-from-floor, ceiling-capped, jittered backoff.
func (p *Poller) backoffInterval(failures int) time.Duration {
	base := PollFloorSeconds
	for i := 1; i < failures && base < PollCeilingSeconds; i++ {
		base *= 2
	}
	if base > PollCeilingSeconds {
		base = PollCeilingSeconds
	}
	return jitter(base, DefaultJitterPct, p.deps.Rand())
}

// ---- internal state ---------------------------------------------------------

func (p *Poller) alreadySeen(taskID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.seen[taskID]
	return ok
}

func (p *Poller) markSeen(taskID string) {
	p.mu.Lock()
	p.seen[taskID] = struct{}{}
	p.mu.Unlock()
}

func (p *Poller) onSuccess(taskCount int) {
	p.mu.Lock()
	p.consecutiveFailures = 0
	p.lastPollSuccess = p.deps.Now()
	p.lastTaskCount = taskCount
	p.mu.Unlock()
}

func (p *Poller) onFailure() {
	p.mu.Lock()
	p.consecutiveFailures++
	p.mu.Unlock()
}

// onPartialFailure records a DEGRADED poll: the poll itself returned 200 but a
// task's ack/report failed transiently. It increments the failure counter (so
// backoff engages) and updates the task count, but does NOT advance
// lastPollSuccess (so Stats surfaces the degraded state).
func (p *Poller) onPartialFailure(taskCount int) {
	p.mu.Lock()
	p.consecutiveFailures++
	p.lastTaskCount = taskCount
	p.mu.Unlock()
}

func (p *Poller) failureCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consecutiveFailures
}

// wait blocks for d or until a poke / ctx; poked=true if a poke woke it.
func (p *Poller) wait(ctx context.Context, d time.Duration) (bool, error) {
	if p.deps.Wait != nil {
		return p.deps.Wait(ctx, d)
	}
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
			return false, nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-t.C:
		return false, nil
	case <-p.deps.Poke: // a nil channel blocks forever -> behaves as a plain sleep
		return true, nil
	}
}

// ---- pure helpers -----------------------------------------------------------

func clampSeconds(s int) int {
	if s < PollFloorSeconds {
		return PollFloorSeconds
	}
	if s > PollCeilingSeconds {
		return PollCeilingSeconds
	}
	return s
}

func clampJitter(pct int) int {
	if pct < 0 {
		return 0
	}
	if pct > maxJitterPct {
		return maxJitterPct
	}
	return pct
}

func jitter(baseSeconds, pct int, r float64) time.Duration {
	delta := (2*r - 1) * float64(pct) / 100.0
	secs := float64(baseSeconds) * (1 + delta)
	return time.Duration(secs * float64(time.Second))
}

func nonEmpty(value []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, errors.New("empty")
	}
	return value, nil
}

// ---- nil-safe operational logging (never receives secrets) ------------------

func (p *Poller) logInfo(event string, kv ...string) {
	if p.deps.Log == nil {
		return
	}
	emitLog(p.deps.Log.Info(event), p.deviceID, kv)
}

func (p *Poller) logWarn(event string, kv ...string) {
	if p.deps.Log == nil {
		return
	}
	emitLog(p.deps.Log.Warn(event), p.deviceID, kv)
}

func (p *Poller) logPollOK(taskCount, processed, skipped, failed int, next time.Duration, work bool) {
	if p.deps.Log == nil {
		return
	}
	p.deps.Log.Info("tasks.poll_ok").
		Str("device_id", p.deviceID).
		Int("task_count", taskCount).
		Int("processed", processed).
		Int("skipped", skipped).
		Int("failed", failed).
		Int("next_seconds", int(next/time.Second)).
		Bool("work_available", work).
		Msg("")
}

func emitLog(ev *zerolog.Event, deviceID string, kv []string) {
	ev = ev.Str("device_id", deviceID)
	for i := 0; i+1 < len(kv); i += 2 {
		ev = ev.Str(kv[i], kv[i+1])
	}
	ev.Msg("")
}
