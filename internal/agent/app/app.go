// Package app is the agent composition root (S1-T19): it constructs and connects
// the foundation collaborators (config, logging, state, audit, identity, the
// pinned transport / saasclient) and the use-cases (enroll, heartbeat, tasks),
// then runs the runtime loops until shutdown.
//
// It is WIRING ONLY — no business logic lives here, and it never bypasses the
// use-cases. Startup: load config → logger → state → identity → pinned saasclient
// (low MaxRetries; TLS/SPKI pinning unchanged) → seed the in-memory token from
// durable state → determine the enrolled state (device_id ∧ agent_certificate_pem
// ∧ agent_session_token; device_id alone is NEVER enrolled). If not enrolled it
// runs enrollment once (S1-T14), then starts the heartbeat (S1-T15) and task-poll
// (S1-T16) loops, forwarding the heartbeat's work_available to the task loop's
// poke. A 401 from the loops surfaces ErrEnrollmentRequired; a 426 surfaces
// ErrUpgradeRequired; context cancellation shuts down gracefully.
//
// Out of scope (per S1-T19): OS service install / Windows SCM, updater/backup/
// restore execution, any real scheduler, and server-side logic.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/heartbeat"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/agent/tasks"
	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/wireversion"
)

// transportMaxRetries is the low retry count for the heartbeat/task transport
// (frozen S1-T15/T16 decision): a missed beat/poll self-heals on the next tick,
// and Retry-After (honored by T12) is the primary fleet-load control.
const transportMaxRetries = 1

// Typed errors. Match with errors.Is.
var (
	ErrConfig             = errors.New("app: config init failed")
	ErrStateInit          = errors.New("app: state store init failed")
	ErrIdentityInit       = errors.New("app: identity init failed")
	ErrTransportInit      = errors.New("app: transport init failed")
	ErrRuntimeInit        = errors.New("app: runtime init failed")
	ErrEnrollFailed       = errors.New("app: enrollment failed")
	ErrEnrollmentRequired = errors.New("app: re-enrollment required (401)")
	ErrUpgradeRequired    = errors.New("app: protocol upgrade required (426)")
)

// Options configures the composition root.
type Options struct {
	// ConfigPath is the config.yaml path (ignored when Config is set).
	ConfigPath string
	// StateDir is the protected state directory.
	StateDir string
	// BootstrapPins are the compiled-in SPKI pins (ADR-005 §0.5) — the trust
	// anchor supplied by the signed entrypoint, NOT read from operator config.
	// Required (the transport fails closed without pins).
	BootstrapPins []string

	// Config, when set, skips the file load (tests / pre-validated config).
	Config *config.Config
	// Protector wraps secrets at rest; nil uses the platform default
	// (Windows DPAPI machine-scope; other platforms fail closed).
	Protector state.Protector
}

// loopRunner is a runnable use-case loop (heartbeat, tasks). Satisfied by
// *heartbeat.Beater and *tasks.Poller.
type loopRunner interface {
	Run(ctx context.Context) error
}

// builder constructs the use-cases over the wired collaborators. prodBuilder is
// the real wiring; tests inject a fake to exercise the run flow.
type builder interface {
	IsEnrolled() (bool, error)
	Enroll(ctx context.Context) error
	Heartbeat(workAvailable chan<- struct{}) (loopRunner, error)
	Tasks(poke <-chan struct{}) (loopRunner, error)
}

// App is the composition root.
type App struct {
	log     *logging.Logger
	b       builder
	closers []func() error // released in reverse (LIFO) by Close
}

// New builds and connects all collaborators, seeds the in-memory session token
// from durable state, and returns a runnable App. The caller must Close it.
func New(opts Options) (*App, error) {
	// 1. Config.
	cfg := opts.Config
	if cfg == nil {
		c, err := config.Load(opts.ConfigPath, config.NewBootstrapLogger())
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConfig, err)
		}
		if err := config.Validate(c); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConfig, err)
		}
		cfg = c
	}

	var closers []func() error
	fail := func(wrap error, err error) (*App, error) {
		closeAll(closers)
		return nil, fmt.Errorf("%w: %v", wrap, err)
	}

	// 2. Logger.
	log, err := logging.NewFromConfig(cfg, "agent")
	if err != nil {
		return nil, fmt.Errorf("%w: logging: %v", ErrConfig, err)
	}
	closers = append(closers, log.Close)

	// 3. State store.
	store, err := state.Open(state.Options{Dir: opts.StateDir, Protector: opts.Protector})
	if err != nil {
		return fail(ErrStateInit, err)
	}
	closers = append(closers, store.Close)

	// 4. Identity manager.
	idm, err := identity.New(store)
	if err != nil {
		return fail(ErrIdentityInit, err)
	}

	// 5. Pinned transport / saasclient (low retry; TLS+SPKI pinning unchanged).
	if len(opts.BootstrapPins) == 0 {
		return fail(ErrTransportInit, errors.New("no SPKI bootstrap pins"))
	}
	hc := httpclient.DefaultConfig()
	hc.MaxRetries = transportMaxRetries
	client, err := saasclient.New(saasclient.Options{
		BaseURL:    cfg.General.APIBaseURL,
		Pins:       opts.BootstrapPins,
		HTTPConfig: &hc,
	})
	if err != nil {
		return fail(ErrTransportInit, err)
	}

	// 6. Seed the in-memory bearer token from durable state (if enrolled already).
	if tok, gerr := store.GetSecret(state.SecretSessionToken); gerr == nil && len(tok) > 0 {
		client.SetSessionToken(string(tok))
	}

	return &App{
		log:     log,
		b:       &prodBuilder{cfg: cfg, log: log, store: store, idm: idm, client: client, stateDir: opts.StateDir},
		closers: closers,
	}, nil
}

// Run ensures enrollment, then runs the heartbeat and task-poll loops until a
// terminal error (401 → ErrEnrollmentRequired, 426 → ErrUpgradeRequired) or
// context cancellation (graceful → nil).
func (a *App) Run(ctx context.Context) error {
	ok, err := a.b.IsEnrolled()
	if err != nil {
		return fmt.Errorf("%w: enrolled check: %v", ErrStateInit, err)
	}
	if !ok {
		a.logInfo("app.enrollment_required")
		if eerr := a.b.Enroll(ctx); eerr != nil {
			// A shutdown signal during the startup enroll is graceful, not a hard
			// failure. The enroll error chain is flattened (so errors.Is won't see
			// the ctx error), hence the direct ctx.Err() check.
			if ctx.Err() != nil {
				return nil
			}
			return classifyEnroll(eerr)
		}
		if ok, err = a.b.IsEnrolled(); err != nil || !ok {
			return fmt.Errorf("%w: device not enrolled after enrollment", ErrEnrollFailed)
		}
		a.logInfo("app.enrolled")
	}

	// work_available (heartbeat) -> poke (tasks). Buffered+coalesced.
	poke := make(chan struct{}, 1)
	hb, err := a.b.Heartbeat(poke)
	if err != nil {
		return fmt.Errorf("%w: heartbeat: %v", ErrRuntimeInit, err)
	}
	tk, err := a.b.Tasks(poke)
	if err != nil {
		return fmt.Errorf("%w: tasks: %v", ErrRuntimeInit, err)
	}
	a.logInfo("app.runtime_started")
	return runLoops(ctx, hb, tk)
}

// Close releases the state store and logger (LIFO).
func (a *App) Close() error { return closeAll(a.closers) }

// Logger returns the app's structured logger (redacting at the sink). May be nil
// before New completes. The service adapter uses it for panic/lifecycle logging.
func (a *App) Logger() *logging.Logger { return a.log }

func (a *App) logInfo(event string) {
	if a.log != nil {
		a.log.Info(event).Msg("")
	}
}

// runLoops runs both loops concurrently. The first terminal error cancels the
// sibling; context cancellation is graceful (nil).
func runLoops(ctx context.Context, hb, tk loopRunner) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errc := make(chan error, 2)
	go func() { errc <- classifyLoop(hb.Run(ctx)) }()
	go func() { errc <- classifyLoop(tk.Run(ctx)) }()

	var firstErr error
	for i := 0; i < 2; i++ {
		if e := <-errc; e != nil && firstErr == nil {
			firstErr = e
			cancel() // stop the sibling loop
		}
	}
	return firstErr
}

// classifyLoop maps a heartbeat/tasks Run error to an app-level outcome.
func classifyLoop(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil // graceful shutdown
	case errors.Is(err, heartbeat.ErrUnauthorized), errors.Is(err, tasks.ErrUnauthorized):
		return fmt.Errorf("%w: %v", ErrEnrollmentRequired, err)
	case errors.Is(err, heartbeat.ErrUpgradeRequired), errors.Is(err, tasks.ErrUpgradeRequired):
		return fmt.Errorf("%w: %v", ErrUpgradeRequired, err)
	default:
		return err
	}
}

// classifyEnroll maps a startup enrollment error to an app-level outcome. A
// rejected/consumed token (401/409) and a protocol upgrade (426) are TERMINAL —
// re-running enrollment with the same token/binary just loops — so they surface
// as the distinct re-enroll/upgrade outcomes the entrypoint maps to non-restart
// exit codes. Any other failure (network/5xx/transient) stays ErrEnrollFailed,
// which exits generic so the service manager may legitimately retry enrollment.
func classifyEnroll(err error) error {
	switch {
	case errors.Is(err, enroll.ErrUpgradeRequired):
		return fmt.Errorf("%w: %v", ErrUpgradeRequired, err)
	case errors.Is(err, enroll.ErrTokenRejected):
		return fmt.Errorf("%w: %v", ErrEnrollmentRequired, err)
	default:
		return fmt.Errorf("%w: %v", ErrEnrollFailed, err)
	}
}

func closeAll(closers []func() error) error {
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ---- enrolled predicate -----------------------------------------------------

// stateReader is the minimal state surface the enrolled predicate needs.
type stateReader interface {
	Get(key string) ([]byte, error)
	GetSecret(key string) ([]byte, error)
}

// enrolled reports whether the device is fully enrolled: device_id AND
// agent_certificate_pem AND agent_session_token must all be present and non-empty
// (FI-1). device_id alone never means ENROLLED. A missing key → (false, nil); a
// real read error → (false, err).
func enrolled(s stateReader) (bool, error) {
	for _, k := range []struct {
		key    string
		secret bool
	}{
		{state.KeyDeviceID, false},
		{state.KeyCertificate, false},
		{state.SecretSessionToken, true},
	} {
		get := s.Get
		if k.secret {
			get = s.GetSecret
		}
		v, err := get(k.key)
		if err != nil {
			if errors.Is(err, state.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if len(v) == 0 {
			return false, nil
		}
	}
	return true, nil
}

// ---- production builder (real wiring) ---------------------------------------

type prodBuilder struct {
	cfg      *config.Config
	log      *logging.Logger
	store    *state.Store
	idm      *identity.Manager
	client   *saasclient.Client
	stateDir string // for the S1-T26 post-update health handshake (marker + health.json)

	runtimeEm *audit.Emitter // shared by heartbeat+tasks (serializes the audit chain)
}

func (pb *prodBuilder) IsEnrolled() (bool, error) { return enrolled(pb.store) }

func (pb *prodBuilder) Enroll(ctx context.Context) error {
	em, err := pb.newEmitter() // fresh, single-threaded emitter for the enroll exchange
	if err != nil {
		return err
	}
	enr, err := enroll.New(enroll.Deps{
		Config:   pb.cfg,
		Identity: pb.idm,
		Client:   pb.client,
		State:    pb.store,
		Audit:    em,
		Log:      pb.log,
	})
	if err != nil {
		return err
	}
	_, err = enr.Enroll(ctx)
	return err
}

func (pb *prodBuilder) Heartbeat(workAvailable chan<- struct{}) (loopRunner, error) {
	em, err := pb.sharedEmitter()
	if err != nil {
		return nil, err
	}
	rep := newPostUpdateReporter(pb.stateDir, pb.log)
	return heartbeat.New(heartbeat.Deps{
		Config:        pb.cfg,
		Client:        pb.client,
		State:         pb.store,
		Audit:         em,
		Log:           pb.log,
		WorkAvailable: workAvailable,
		UpdateReport:  rep.report,
		OnBeatSuccess: rep.onBeatSuccess,
	})
}

func (pb *prodBuilder) Tasks(poke <-chan struct{}) (loopRunner, error) {
	em, err := pb.sharedEmitter()
	if err != nil {
		return nil, err
	}
	return tasks.New(tasks.Deps{
		Config: pb.cfg,
		Client: pb.client,
		State:  pb.store,
		Audit:  em,
		Log:    pb.log,
		Poke:   poke,
	})
}

// sharedEmitter builds the runtime audit emitter once and reuses it, so the
// concurrent heartbeat and task loops append to the hash chain through a single
// mutex-guarded emitter (no sequence collisions).
func (pb *prodBuilder) sharedEmitter() (*audit.Emitter, error) {
	if pb.runtimeEm != nil {
		return pb.runtimeEm, nil
	}
	em, err := pb.newEmitter()
	if err != nil {
		return nil, err
	}
	pb.runtimeEm = em
	return em, nil
}

// newEmitter builds an audit emitter anchored to the device GUID, carrying the
// currently-persisted identity (device_id/tenant_id are empty pre-enrollment).
func (pb *prodBuilder) newEmitter() (*audit.Emitter, error) {
	guid, err := pb.idm.EnsureDeviceGUID()
	if err != nil {
		return nil, err
	}
	return audit.New(pb.store.AuditAppender(), nil, audit.Identity{
		DeviceGUID:   guid,
		DeviceID:     pb.getPlain(state.KeyDeviceID),
		TenantID:     pb.getPlain(state.KeyTenantID),
		ParentOrgID:  pb.getPlain(state.KeyParentOrgID),
		AgentVersion: wireversion.AgentVersion(),
		Source:       audit.SourceAgent,
	})
}

func (pb *prodBuilder) getPlain(key string) string {
	if v, err := pb.store.Get(key); err == nil {
		return string(v)
	}
	return ""
}
