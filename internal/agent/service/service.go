// Package service is the agent OS service-lifecycle adapter (S1-T18): a thin
// wrapper over github.com/kardianos/service that runs the agent workload under
// the Windows SCM, Linux systemd, or a foreground console, with one graceful
// shutdown path, panic recovery, and a single-instance guard.
//
// It is WIRING ONLY — no business logic, app-agnostic (it supervises any
// Runnable). The frozen contract (S1-T18):
//   - Start spawns Run(ctx) in a goroutine; Stop cancels the context, WAITS for
//     Run to return, then Closes — Close is called only AFTER Run exits.
//   - Console SIGINT/SIGTERM and the service-manager Stop funnel through the SAME
//     path (kardianos delivers both as Interface.Stop).
//   - A panic in the run goroutine is recovered (logged via the redacting logger,
//     never a struct/secret dump) and surfaced as ErrPanic — no silent crash.
//   - An explicit lockfile rejects a second instance with a clear error; bbolt's
//     exclusive open-lock is the hard backstop (the agent is the sole bbolt writer).
//
// Service identity: name BeyzBackupAgent (frozen; Phase-3 rename deferred), run as
// LocalSystem on Windows (kardianos default when UserName is empty). Out of scope:
// the installer (T29), the systemd unit file (T20), the updater service, and any
// backup/restore engine.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	kservice "github.com/kardianos/service"

	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
)

// DefaultName is the OS service name (frozen identifier; Phase-3 rename deferred).
const DefaultName = "BeyzBackupAgent"

const (
	// defaultStopGrace bounds how long a service-mode self-end waits for a clean
	// manager stop before forcing a last-resort exit, so a failed/blocked stop
	// round-trip can never leave the agent hung holding the lock.
	defaultStopGrace = 15 * time.Second
	// exitFallback is the exit code used when no ExitFunc is configured.
	exitFallback = 1
)

// osExit is os.Exit, indirected for tests. Used only by the service-mode watchdog
// as a last resort when a clean manager stop cannot terminate the process.
var osExit = os.Exit

var (
	// ErrNilRunnable is returned by New when no Runnable is supplied.
	ErrNilRunnable = errors.New("service: nil runnable")
	// ErrPanic wraps a recovered panic from the run goroutine.
	ErrPanic = errors.New("service: run goroutine panicked")
)

// Runnable is the long-running workload the service supervises. *app.App satisfies it.
type Runnable interface {
	Run(ctx context.Context) error
	Close() error
}

// Config configures the service adapter.
type Config struct {
	// Name is the OS service name; defaults to DefaultName (BeyzBackupAgent).
	Name        string
	DisplayName string
	Description string
	// Runnable is the supervised workload (required).
	Runnable Runnable
	// Log is optional; it receives panic/lifecycle events and redacts secrets at
	// the sink. nil disables service-level logging (the panic is still surfaced).
	Log *logging.Logger
	// LockPath is the single-instance lockfile path; "" disables the explicit guard
	// (bbolt's exclusive lock remains the hard backstop).
	LockPath string
	// Arguments are recorded in the installed service's run command.
	Arguments []string
	// ExitFunc maps a terminal workload error to a process exit code. It is used
	// ONLY by the service-mode watchdog's last-resort exit; nil falls back to a
	// generic non-zero code.
	ExitFunc func(error) int
	// StopGrace overrides the watchdog grace window (defaultStopGrace when zero).
	StopGrace time.Duration
}

// Service is the OS-service lifecycle adapter.
type Service struct {
	prog     *program
	ksvc     kservice.Service
	lockPath string
}

// New builds the adapter over kardianos/service. UserName is left empty so the
// installed Windows service runs as LocalSystem.
func New(cfg Config) (*Service, error) {
	if cfg.Runnable == nil {
		return nil, ErrNilRunnable
	}
	name := cfg.Name
	if name == "" {
		name = DefaultName
	}
	prog := &program{
		run:     cfg.Runnable.Run,
		closeFn: cfg.Runnable.Close,
		log:     cfg.Log,
		exitFn:  cfg.ExitFunc,
		grace:   cfg.StopGrace,
	}
	ksvc, err := kservice.New(prog, &kservice.Config{
		Name:        name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		Arguments:   cfg.Arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("service: build: %w", err)
	}
	return &Service{prog: prog, ksvc: ksvc, lockPath: cfg.LockPath}, nil
}

// Run acquires the single-instance lock and runs the service, returning the
// workload's terminal error (nil on graceful shutdown). The Runnable is Closed
// exactly once on every path.
//
// In foreground/console mode (Interactive) the workload is run DIRECTLY under a
// SIGINT/SIGTERM-cancelled context: a console signal cancels it, and a self-end
// (e.g. terminal 401/426) returns immediately — no service-manager round-trip,
// so the process always exits with the mapped code. Under a service manager
// (Windows SCM / systemd) kardianos owns the run loop; a self-end requests a
// clean OS stop so the manager records a graceful exit (no failure auto-restart).
func (s *Service) Run() error {
	if s.lockPath != "" {
		lock, err := acquireLock(s.lockPath)
		if err != nil {
			_ = s.prog.doClose() // never ran; release the workload's resources
			return err
		}
		defer func() { _ = lock.release() }()
	}
	if Interactive() {
		return s.runForeground()
	}
	if err := s.ksvc.Run(); err != nil {
		_ = s.prog.doClose() // SCM/systemd never reached Start/Stop -> close here
		return fmt.Errorf("service: run: %w", err)
	}
	return s.prog.result()
}

// runForeground runs the workload in the current process under a signal-cancelled
// context (the foreground equivalent of the service-manager Stop path).
func (s *Service) runForeground() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.runWith(ctx)
}

// runWith runs the workload to completion (self-end or ctx cancellation), then
// Closes — the single foreground shutdown path. Separated for testability.
func (s *Service) runWith(ctx context.Context) error {
	err := s.prog.runWithRecover(ctx)
	if cerr := s.prog.doClose(); cerr != nil && err == nil {
		err = cerr
	}
	s.prog.mu.Lock()
	s.prog.exitErr = err
	s.prog.mu.Unlock()
	return err
}

// Control performs an OS service-manager action: "install", "uninstall", "start",
// "stop", or "restart".
func (s *Service) Control(action string) error {
	return kservice.Control(s.ksvc, action)
}

// ServiceStatus is the OS service's run state, as observed by the service manager.
type ServiceStatus int

const (
	// StatusUnknown means the state could not be determined (e.g. not installed,
	// or a query error).
	StatusUnknown ServiceStatus = iota
	// StatusRunning means the service is running.
	StatusRunning
	// StatusStopped means the service is installed but not running.
	StatusStopped
)

func (s ServiceStatus) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Status reports the service's current run state (Running/Stopped/Unknown). The
// updater's post-update health gate (S1-T26) uses it as one of the two required
// pass conditions. It fails CLOSED: any query error yields StatusUnknown plus the
// error, so a transient SCM/systemd failure can never be misread as Running.
func (s *Service) Status() (ServiceStatus, error) {
	st, err := s.ksvc.Status()
	if err != nil {
		return StatusUnknown, err
	}
	switch st {
	case kservice.StatusRunning:
		return StatusRunning, nil
	case kservice.StatusStopped:
		return StatusStopped, nil
	default:
		return StatusUnknown, nil
	}
}

// Interactive reports whether the process runs in the foreground (console) rather
// than under a service manager.
func Interactive() bool { return kservice.Interactive() }

// ---- program: kardianos Interface + testable internals ----------------------

type program struct {
	run     func(ctx context.Context) error
	closeFn func() error
	log     *logging.Logger
	exitFn  func(error) int // maps a terminal error -> exit code (watchdog fallback)
	grace   time.Duration   // watchdog grace window; defaultStopGrace when zero

	cancel      context.CancelFunc
	done        chan error
	stopped     chan struct{}
	requestStop func()

	closeOnce    sync.Once
	closeErr     error
	finalizeOnce sync.Once
	stoppedOnce  sync.Once

	mu      sync.Mutex
	exitErr error
}

// Start (kardianos Interface, service mode only) — must return promptly; it spawns
// the workload. On a self-end it asks the service manager to stop us (a clean stop,
// so SCM/systemd records a graceful exit rather than a failure that would
// auto-restart). A failed stop request is logged, not swallowed, and the watchdog
// (see start) guarantees the process still exits. Foreground runs never reach here
// (Service.Run takes the direct runForeground path).
func (p *program) Start(s kservice.Service) error {
	return p.start(func() {
		if err := s.Stop(); err != nil && p.log != nil {
			p.log.Warn("service.self_stop_failed").Str("error", err.Error()).Msg("")
		}
	})
}

// Stop (kardianos Interface) — must return promptly; cancels + waits + closes.
func (p *program) Stop(_ kservice.Service) error { return p.stop() }

// start spawns the workload. When the workload self-ends (NOT a manager-initiated
// cancel) it asks the manager for a clean stop, which routes back through Stop so
// SCM/systemd records a graceful exit (no failure auto-restart). That request is
// best-effort and runs in its own goroutine so a blocked/failed round-trip cannot
// wedge us; a watchdog then forces a last-resort exit (with the mapped code, after
// Close) if no clean stop completes within the grace window — so a failed manager
// round-trip can never leave the agent hung holding the lock.
func (p *program) start(requestStop func()) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.requestStop = requestStop
	p.done = make(chan error, 1)
	p.stopped = make(chan struct{})
	go func() {
		err := p.runWithRecover(ctx)
		p.done <- err
		if ctx.Err() != nil {
			return // manager-initiated stop; program.stop() finalizes
		}
		if p.requestStop != nil {
			go p.requestStop() // best-effort clean stop; may block or fail
		}
		grace := p.grace
		if grace <= 0 {
			grace = defaultStopGrace
		}
		select {
		case <-p.stopped: // a clean manager stop completed
		case <-time.After(grace):
			p.finalize()
			code := exitFallback
			if p.exitFn != nil {
				code = p.exitFn(p.result())
			}
			if p.log != nil {
				p.log.Error("service.forced_exit").Int("code", code).
					Msg("clean stop did not complete within grace; forcing exit")
			}
			osExit(code)
		}
	}()
	return nil
}

// stop is the manager-stop path: cancel the workload context, wait for Run to
// return, then Close (after Run exits). Shared by console signals and the
// service-manager Stop; it also releases the self-end watchdog.
func (p *program) stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.finalize()
	if p.stopped != nil {
		p.stoppedOnce.Do(func() { close(p.stopped) })
	}
	return nil
}

// finalize drains the run result and Closes exactly once — the workload's Close
// runs only after Run has returned.
func (p *program) finalize() {
	p.finalizeOnce.Do(func() {
		err := <-p.done
		if cerr := p.doClose(); cerr != nil && err == nil {
			err = cerr
		}
		p.mu.Lock()
		p.exitErr = err
		p.mu.Unlock()
	})
}

// runWithRecover runs the workload, converting a panic into ErrPanic (logged via
// the redacting sink — only the panic value, never a struct/secret dump).
func (p *program) runWithRecover(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if p.log != nil {
				p.log.Error("service.panic").Str("panic", fmt.Sprint(r)).Msg("")
			}
			err = fmt.Errorf("%w: %v", ErrPanic, r)
		}
	}()
	return p.run(ctx)
}

// doClose closes the workload at most once.
func (p *program) doClose() error {
	p.closeOnce.Do(func() { p.closeErr = p.closeFn() })
	return p.closeErr
}

// result returns the workload's terminal error after a run-then-stop cycle.
func (p *program) result() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}
