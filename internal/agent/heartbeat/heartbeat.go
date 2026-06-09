// Package heartbeat implements the agent presence heartbeat use-case (S1-T15): a
// thin, server-directed, jittered, low-retry loop that announces device presence
// and persists any rotated session token.
//
// It is a single-purpose USE-CASE layer — NOT a general scheduler. It does no
// task polling (S1-T16), backup/restore, or business logic in the SaaS client.
// Frozen design (senior review, 2026-06-09):
//   - Cadence is SERVER-DIRECTED: HeartbeatResponse.NextHeartbeatSeconds drives
//     the next interval, clamped to [FloorSeconds=60, CeilingSeconds=3600] to
//     defend against a bad/hostile server value; the config interval is the
//     pre-first-response fallback. Mandatory jitter ±poll_jitter_pct (default 20%).
//   - LOW retry: the transport must be configured with MaxRetries=1 (HeartbeatMaxRetries);
//     a missed beat self-heals on the next tick, and Retry-After (honored by T12)
//     is the primary fleet-load control.
//   - Token rotation: a rotated agent_session_token is persisted (state.PutSecret)
//     as part of completing a beat. On persist failure the agent CONTINUES on the
//     in-memory token (the only currently-valid one), retries the persist on future
//     beats, and logs the durability gap — it does NOT stop the loop (T13-C2).
//   - Enrolled predicate: the loop only runs when device_id AND agent_certificate_pem
//     AND agent_session_token are all present; device_id alone never means ENROLLED
//     (FI-1). A partial enrollment must not start heartbeating.
//   - Errors: 401 -> audit auth.failure + STOP (re-enroll); 426 -> STOP (route to
//     updater, ADR-004); transport/5xx/429 -> exponential backoff + jitter
//     (circuit-breaker) and keep looping. Secrets are never logged.
//
// Routine heartbeats are LOGGED, not audited: the frozen T09 audit vocabulary has
// no heartbeat/session/rotation event, and auditing fleet-scale presence telemetry
// would swamp the hash-chained security log. The only heartbeat-path audit is the
// existing auth.failure on a 401.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
	"github.com/beyzbackup/beyz-backup/pkg/wireversion"
)

// Frozen cadence/retry constants (senior review, 2026-06-09).
const (
	FloorSeconds        = 60   // minimum effective heartbeat interval
	CeilingSeconds      = 3600 // maximum effective heartbeat interval
	DefaultJitterPct    = 20   // default jitter when the server provides none
	maxJitterPct        = 50   // matches the OpenAPI poll_jitter_pct ceiling
	HeartbeatMaxRetries = 1    // low retry for the heartbeat transport
)

// Typed errors. Match with errors.Is.
var (
	// ErrNotEnrolled means the device is not fully enrolled (device_id +
	// certificate + session_token must all be present); the loop must not start.
	ErrNotEnrolled = errors.New("heartbeat: device not enrolled")
	// ErrUnauthorized is a 401: the session token is invalid/revoked. TERMINAL —
	// the loop stops and the caller routes to re-enrollment (§0.2).
	ErrUnauthorized = errors.New("heartbeat: session token rejected (401)")
	// ErrUpgradeRequired is a 426. TERMINAL — the loop stops and the caller routes
	// to the updater (ADR-004).
	ErrUpgradeRequired = errors.New("heartbeat: protocol upgrade required (426)")
	// ErrBeatFailed is a transient failure (transport/5xx/429 after low retry). The
	// loop backs off and continues.
	ErrBeatFailed = errors.New("heartbeat: transient beat failure")
)

// HeartbeatClient is the typed transport (S1-T13). Satisfied by *saasclient.Client.
type HeartbeatClient interface {
	Heartbeat(ctx context.Context, deviceID string, body proto.HeartbeatRequest) (*proto.HeartbeatResponse, error)
	// SetSessionToken seeds the in-memory bearer cache from durable state at startup.
	SetSessionToken(token string)
}

// StateStore reads the enrolled credential and persists rotated tokens.
// Satisfied by *state.Store. Secret keys go through GetSecret/PutSecret.
type StateStore interface {
	Get(key string) ([]byte, error)
	GetSecret(key string) ([]byte, error)
	PutSecret(key string, value []byte) error
}

// AuditEmitter records hash-chained audit events (S1-T09). Satisfied by *audit.Emitter.
type AuditEmitter interface {
	Emit(ev audit.Event) (audit.Record, error)
}

// Deps bundles the heartbeat collaborators and inputs.
type Deps struct {
	Config *config.Config
	Client HeartbeatClient
	State  StateStore
	Audit  AuditEmitter
	// Log is optional (nil disables operational logging). It never receives secrets.
	Log *logging.Logger
	// Now/Sleep/Rand are injectable for tests; nil uses production defaults.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Rand  func() float64 // jitter source in [0,1)
}

// Stats is the observability snapshot (forward hook: heartbeat_duration_ms,
// heartbeat_last_success, heartbeat_consecutive_failures).
type Stats struct {
	LastSuccess         time.Time
	ConsecutiveFailures int
	LastDurationMS      int64
}

// BeatResult is the outcome of a single heartbeat.
type BeatResult struct {
	NextInterval  time.Duration // server-directed, clamped, jittered
	WorkAvailable bool          // hint for task polling (S1-T16); ignored here
	TokenRotated  bool
	DurabilityGap bool  // a rotated token could not be persisted (continuing on in-memory)
	DurationMS    int64 // heartbeat_duration_ms
}

// Beater is the heartbeat use-case.
type Beater struct {
	deps     Deps
	deviceID string

	mu                  sync.Mutex
	pendingToken        string // rotated token not yet durably persisted (retried each beat)
	lastSuccess         time.Time
	consecutiveFailures int
	lastDurationMS      int64
}

// New validates dependencies, enforces the enrolled predicate, seeds the client's
// in-memory token from durable state, and returns a Beater. It returns
// ErrNotEnrolled unless device_id, agent_certificate_pem, and agent_session_token
// are all present.
func New(deps Deps) (*Beater, error) {
	switch {
	case deps.Config == nil:
		return nil, errors.New("heartbeat: nil config")
	case deps.Client == nil:
		return nil, errors.New("heartbeat: nil client")
	case deps.State == nil:
		return nil, errors.New("heartbeat: nil state")
	case deps.Audit == nil:
		return nil, errors.New("heartbeat: nil audit")
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.Sleep == nil {
		deps.Sleep = sleepCtx
	}
	if deps.Rand == nil {
		deps.Rand = rand.Float64 // per-process auto-seeded (Go 1.20+); de-synchronizes the fleet
	}

	// Enrolled predicate (FI-1): device_id AND certificate AND session_token.
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

	// Seed the transport's in-memory bearer cache from durable state (T13 bridge).
	deps.Client.SetSessionToken(string(token))

	return &Beater{deps: deps, deviceID: string(deviceID)}, nil
}

// Beat performs one heartbeat: it sends the presence payload, persists any rotated
// token (retrying a prior unpersisted one), classifies the result, and returns the
// next (clamped, jittered) interval. Terminal errors (401/426) are returned with
// the typed sentinels; transient errors return ErrBeatFailed.
func (b *Beater) Beat(ctx context.Context) (*BeatResult, error) {
	start := b.deps.Now()
	req := proto.HeartbeatRequest{
		AgentVersion:    proto.AgentVersion(wireversion.AgentVersion()),
		ProtocolVersion: proto.ProtocolVersion(wireversion.CurrentProtocolVersion),
		Status:          proto.Idle, // Sprint 1: no backup/restore/update engine running
	}

	resp, err := b.deps.Client.Heartbeat(ctx, b.deviceID, req)
	durMS := b.deps.Now().Sub(start).Milliseconds()
	b.setLastDuration(durMS)
	if err != nil {
		return nil, b.handleError(err, durMS)
	}
	if resp == nil {
		// Defensive: the real client never returns (nil, nil) (a bodyless 2xx is
		// ErrEmptyBody), but route this transient case through the same failure
		// accounting as every other ErrBeatFailed path.
		b.onFailure()
		b.logWarn("heartbeat.failed", "duration_ms_note", "empty_response")
		return nil, fmt.Errorf("%w: empty response", ErrBeatFailed)
	}

	// Token rotation: persist before the beat is durably complete. On failure,
	// keep the in-memory token (already cached by the client) and retry later.
	rotated, gap := b.persistRotation(resp)

	next := b.nextInterval(resp.NextHeartbeatSeconds, resp.PollJitterPct)
	b.onSuccess()
	b.logBeatOK(durMS, next, resp.WorkAvailable, rotated, gap)

	return &BeatResult{
		NextInterval:  next,
		WorkAvailable: resp.WorkAvailable,
		TokenRotated:  rotated,
		DurabilityGap: gap,
		DurationMS:    durMS,
	}, nil
}

// Run is the single-purpose heartbeat loop. It applies a bounded jittered startup
// delay (herd mitigation), then beats on the server-directed cadence, backing off
// with jitter on transient failures (circuit-breaker) and stopping on a terminal
// error (401/426) or context cancellation.
func (b *Beater) Run(ctx context.Context) error {
	interval := b.startupDelay()
	for {
		if err := b.deps.Sleep(ctx, interval); err != nil {
			return ctx.Err()
		}
		res, err := b.Beat(ctx)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUpgradeRequired) {
				return err // terminal — caller routes to re-enroll / updater
			}
			interval = b.backoffInterval(b.failureCount())
			continue
		}
		interval = res.NextInterval
	}
}

// Stats returns the current observability snapshot.
func (b *Beater) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{
		LastSuccess:         b.lastSuccess,
		ConsecutiveFailures: b.consecutiveFailures,
		LastDurationMS:      b.lastDurationMS,
	}
}

// ---- internals --------------------------------------------------------------

// persistRotation persists a newly rotated token (or retries a previously
// unpersisted one). Returns whether a rotation occurred and whether a durability
// gap remains (persist failed; continuing on the in-memory token).
func (b *Beater) persistRotation(resp *proto.HeartbeatResponse) (rotated, gap bool) {
	b.mu.Lock()
	if resp.AgentSessionToken != nil {
		rotated = true
		b.pendingToken = *resp.AgentSessionToken // newest rotation supersedes any pending
	}
	tok := b.pendingToken
	b.mu.Unlock()

	if tok == "" {
		return rotated, false
	}
	if err := b.deps.State.PutSecret(state.SecretSessionToken, []byte(tok)); err != nil {
		// Durability gap: the client's in-memory cache already holds tok (the only
		// currently-valid token), so the agent keeps running; retry next beat.
		b.logWarn("heartbeat.token_persist_failed", "consecutive_failures_note", "durability_gap")
		return rotated, true
	}
	b.mu.Lock()
	b.pendingToken = ""
	b.mu.Unlock()
	if rotated {
		b.logInfo("heartbeat.token_rotated")
	} else {
		b.logInfo("heartbeat.token_persist_recovered")
	}
	return rotated, false
}

// handleError maps a transport error to a typed heartbeat error, auditing a 401.
func (b *Beater) handleError(err error, durMS int64) error {
	b.onFailure()
	switch {
	case errors.Is(err, saasclient.ErrUnauthorized):
		// 401: token invalid/revoked — the only heartbeat-path audit event.
		b.emitAuthFailure()
		b.logWarn("heartbeat.auth_failure", "duration_ms_note", "terminal")
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	case errors.Is(err, saasclient.ErrUpgradeRequired):
		b.logWarn("heartbeat.upgrade_required", "duration_ms_note", "terminal")
		return fmt.Errorf("%w: %v", ErrUpgradeRequired, err)
	default:
		b.logWarn("heartbeat.failed", "duration_ms_note", "transient")
		return fmt.Errorf("%w: %v", ErrBeatFailed, err)
	}
}

func (b *Beater) emitAuthFailure() {
	_, _ = b.deps.Audit.Emit(audit.Event{
		EventType: audit.EventAuthFailure,
		Category:  audit.CategoryAuth,
		Severity:  audit.SeverityWarn,
		Outcome:   audit.OutcomeDenied,
		Actor:     "system",
		Detail:    map[string]any{"reason": "heartbeat_401", "device_id": b.deviceID},
	})
}

// nextInterval clamps the server cadence to [FloorSeconds, CeilingSeconds] and
// applies ±jitter%.
func (b *Beater) nextInterval(serverSeconds int, pollJitterPct *int) time.Duration {
	base := clampSeconds(serverSeconds)
	jit := DefaultJitterPct
	if pollJitterPct != nil {
		jit = clampJitter(*pollJitterPct)
	}
	return jitter(base, jit, b.deps.Rand())
}

// startupDelay is a bounded jittered delay before the first beat, spreading a
// fleet-restart herd over the floor's jitter window.
func (b *Beater) startupDelay() time.Duration {
	base := clampSeconds(b.deps.Config.Heartbeat.HeartbeatIntervalSeconds)
	// 0 .. base*DefaultJitterPct/100 (e.g. 0..12s for a 60s floor).
	maxDelay := float64(base) * float64(DefaultJitterPct) / 100.0
	return time.Duration(b.deps.Rand() * maxDelay * float64(time.Second))
}

// backoffInterval is an exponential-from-floor, ceiling-capped, jittered backoff
// (the circuit-breaker widening on consecutive failures).
func (b *Beater) backoffInterval(failures int) time.Duration {
	base := FloorSeconds
	for i := 1; i < failures && base < CeilingSeconds; i++ {
		base *= 2
	}
	if base > CeilingSeconds {
		base = CeilingSeconds
	}
	return jitter(base, DefaultJitterPct, b.deps.Rand())
}

func (b *Beater) onSuccess() {
	b.mu.Lock()
	b.consecutiveFailures = 0
	b.lastSuccess = b.deps.Now()
	b.mu.Unlock()
}

func (b *Beater) onFailure() {
	b.mu.Lock()
	b.consecutiveFailures++
	b.mu.Unlock()
}

func (b *Beater) failureCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.consecutiveFailures
}

func (b *Beater) setLastDuration(ms int64) {
	b.mu.Lock()
	b.lastDurationMS = ms
	b.mu.Unlock()
}

// ---- pure helpers -----------------------------------------------------------

func clampSeconds(s int) int {
	if s < FloorSeconds {
		return FloorSeconds
	}
	if s > CeilingSeconds {
		return CeilingSeconds
	}
	return s
}

func clampJitter(p int) int {
	if p < 0 {
		return 0
	}
	if p > maxJitterPct {
		return maxJitterPct
	}
	return p
}

// jitter returns baseSeconds scaled by a factor in [1-pct%, 1+pct%) using r in [0,1).
func jitter(baseSeconds, pct int, r float64) time.Duration {
	delta := (2*r - 1) * float64(pct) / 100.0
	secs := float64(baseSeconds) * (1 + delta)
	return time.Duration(secs * float64(time.Second))
}

// nonEmpty returns (value, nil) only when err is nil and value is non-empty.
func nonEmpty(value []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, errors.New("empty")
	}
	return value, nil
}

// sleepCtx waits d or until ctx is done (returning ctx.Err()).
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
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

// ---- nil-safe operational logging (never receives secrets) ------------------

func (b *Beater) logInfo(event string, kv ...string) {
	if b.deps.Log == nil {
		return
	}
	emitLog(b.deps.Log.Info(event), b.deviceID, kv)
}

func (b *Beater) logWarn(event string, kv ...string) {
	if b.deps.Log == nil {
		return
	}
	emitLog(b.deps.Log.Warn(event), b.deviceID, kv)
}

func (b *Beater) logBeatOK(durMS int64, next time.Duration, work, rotated, gap bool) {
	if b.deps.Log == nil {
		return
	}
	b.deps.Log.Info("heartbeat.ok").
		Str("device_id", b.deviceID).
		Int64("duration_ms", durMS).
		Int("next_seconds", int(next/time.Second)).
		Bool("work_available", work).
		Bool("token_rotated", rotated).
		Bool("durability_gap", gap).
		Msg("")
}

// emitLog appends device_id + string key/value pairs and sends the event. Callers
// pass only non-secret values; the logging package also redacts tokens at the sink.
func emitLog(ev *zerolog.Event, deviceID string, kv []string) {
	ev = ev.Str("device_id", deviceID)
	for i := 0; i+1 < len(kv); i += 2 {
		ev = ev.Str(kv[i], kv[i+1])
	}
	ev.Msg("")
}
