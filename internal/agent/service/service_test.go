package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
)

type fakeRunnable struct {
	runFn func(ctx context.Context) error

	mu            sync.Mutex
	ran           bool
	closed        bool
	closeAfterRun bool
}

func (r *fakeRunnable) Run(ctx context.Context) error {
	err := r.runFn(ctx)
	r.mu.Lock()
	r.ran = true
	r.mu.Unlock()
	return err
}

func (r *fakeRunnable) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeAfterRun = r.ran
	r.closed = true
	return nil
}

func (r *fakeRunnable) state() (ran, closed, closeAfterRun bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ran, r.closed, r.closeAfterRun
}

func progFor(r Runnable, log *logging.Logger) *program {
	return &program{run: r.Run, closeFn: r.Close, log: log}
}

// ---- lifecycle --------------------------------------------------------------

func TestStartStopGracefulAndCloseAfterRun(t *testing.T) {
	started := make(chan struct{})
	r := &fakeRunnable{runFn: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil // graceful
	}}
	p := progFor(r, nil)
	if err := p.start(func() {}); err != nil {
		t.Fatal(err)
	}
	<-started // Run started
	if err := p.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if p.result() != nil {
		t.Errorf("graceful result should be nil, got %v", p.result())
	}
	ran, closed, closeAfterRun := r.state()
	if !ran || !closed || !closeAfterRun {
		t.Errorf("ran=%v closed=%v closeAfterRun=%v — Close must happen after Run exits", ran, closed, closeAfterRun)
	}
}

func TestStopCancelsContextAndWaits(t *testing.T) {
	releaseObserved := make(chan struct{})
	r := &fakeRunnable{runFn: func(ctx context.Context) error {
		<-ctx.Done() // proves Stop cancelled the context
		close(releaseObserved)
		return nil
	}}
	p := progFor(r, nil)
	_ = p.start(func() {})
	_ = p.stop() // blocks until Run returns
	select {
	case <-releaseObserved:
	default:
		t.Error("Stop returned before Run observed the cancellation")
	}
}

func TestSelfEndPropagatesTerminalError(t *testing.T) {
	sentinel := errors.New("terminal-401-style")
	stopReq := make(chan struct{}, 1)
	r := &fakeRunnable{runFn: func(context.Context) error { return sentinel }}
	p := progFor(r, nil)
	_ = p.start(func() { stopReq <- struct{}{} })
	<-stopReq // the workload ended itself and requested a clean service stop
	_ = p.stop()
	if !errors.Is(p.result(), sentinel) {
		t.Errorf("terminal error not preserved: %v", p.result())
	}
}

func TestPanicRecovery(t *testing.T) {
	stopReq := make(chan struct{}, 1)
	r := &fakeRunnable{runFn: func(context.Context) error { panic("boom") }}
	p := progFor(r, nil)
	_ = p.start(func() { stopReq <- struct{}{} })
	<-stopReq
	_ = p.stop()
	if !errors.Is(p.result(), ErrPanic) {
		t.Errorf("panic not recovered as ErrPanic: %v", p.result())
	}
}

func TestNoSecretLeakOnPanic(t *testing.T) {
	var buf bytes.Buffer
	lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	stopReq := make(chan struct{}, 1)
	r := &fakeRunnable{runFn: func(context.Context) error { panic("token ast_secretpanic123 leaked") }}
	p := progFor(r, lg)
	_ = p.start(func() { stopReq <- struct{}{} })
	<-stopReq
	_ = p.stop()
	if strings.Contains(buf.String(), "secretpanic123") {
		t.Errorf("panic log leaked a secret: %s", buf.String())
	}
}

// ---- foreground path (Interactive==true under `go test`) --------------------

// A self-ending workload (e.g. terminal 401/426) must make foreground Run return
// its terminal error and Close — NOT hang on a service-manager round-trip.
func TestServiceRunForegroundSelfEnd(t *testing.T) {
	sentinel := errors.New("terminal-401-style")
	r := &fakeRunnable{runFn: func(context.Context) error { return sentinel }}
	svc, err := New(Config{Name: "test", Runnable: r}) // no lock; foreground
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.Run() }()
	select {
	case got := <-done:
		if !errors.Is(got, sentinel) {
			t.Errorf("foreground self-end err = %v, want sentinel", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("foreground Run hung on a self-ending workload")
	}
	if _, closed, closeAfterRun := r.state(); !closed || !closeAfterRun {
		t.Error("Close must run after the foreground workload exits")
	}
}

// runWith is the foreground shutdown path; a cancelled context yields a graceful
// return + Close (mirrors a console SIGINT/SIGTERM).
func TestServiceRunWithCancellationGraceful(t *testing.T) {
	r := &fakeRunnable{runFn: func(ctx context.Context) error { <-ctx.Done(); return nil }}
	svc, err := New(Config{Name: "test", Runnable: r})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // signal already delivered
	if err := svc.runWith(ctx); err != nil {
		t.Errorf("graceful cancel must return nil, got %v", err)
	}
	if _, closed, _ := r.state(); !closed {
		t.Error("Close must run on the foreground cancellation path")
	}
}

// A service-mode self-end where the clean manager stop never completes (s.Stop
// fails/hangs) must NOT leave the process hung: the watchdog forces a last-resort
// exit with the mapped code, after Close.
func TestServiceModeWatchdogForcesExitOnStuckStop(t *testing.T) {
	codeCh := make(chan int, 1)
	old := osExit
	osExit = func(code int) { codeCh <- code; runtime.Goexit() }
	defer func() { osExit = old }()

	sentinel := errors.New("terminal")
	r := &fakeRunnable{runFn: func(context.Context) error { return sentinel }}
	p := &program{
		run:     r.Run,
		closeFn: r.Close,
		grace:   20 * time.Millisecond,
		exitFn:  func(err error) int { return map[bool]int{true: 10, false: 1}[errors.Is(err, sentinel)] },
	}
	_ = p.start(func() {}) // no-op requestStop -> a clean stop never arrives

	select {
	case code := <-codeCh:
		if code != 10 {
			t.Errorf("forced exit code = %d, want 10 (mapped from the terminal error)", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not force exit when the clean stop never completed")
	}
	if _, closed, _ := r.state(); !closed {
		t.Error("watchdog must Close (release resources/lock) before forcing exit")
	}
}

// A clean manager stop within the grace window must NOT trigger the watchdog exit.
func TestServiceModeCleanStopSkipsWatchdog(t *testing.T) {
	old := osExit
	osExit = func(int) { t.Error("watchdog forced exit despite a clean manager stop"); runtime.Goexit() }
	defer func() { osExit = old }()

	sentinel := errors.New("terminal")
	stopReq := make(chan struct{}, 1)
	r := &fakeRunnable{runFn: func(context.Context) error { return sentinel }}
	p := &program{run: r.Run, closeFn: r.Close, grace: 5 * time.Second}
	_ = p.start(func() { stopReq <- struct{}{} })
	<-stopReq
	_ = p.stop() // the manager delivers a clean stop -> watchdog must take the <-stopped path
	if !errors.Is(p.result(), sentinel) {
		t.Errorf("result = %v, want sentinel", p.result())
	}
	time.Sleep(30 * time.Millisecond) // give any erroneous watchdog exit a chance to fire
}

// ---- single-instance lock ---------------------------------------------------

func TestSingleInstanceLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")
	l1, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := acquireLock(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second lock should be ErrAlreadyRunning, got %v", err)
	}
	if err := l1.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	l2, err := acquireLock(path) // re-acquirable after release
	if err != nil {
		t.Errorf("lock after release should succeed, got %v", err)
	}
	_ = l2.release()
}

func TestServiceRunClosesOnLockContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	held, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()

	r := &fakeRunnable{runFn: func(context.Context) error { return nil }}
	svc, err := New(Config{Name: "test", Runnable: r, LockPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Run(); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Run should return ErrAlreadyRunning, got %v", err)
	}
	if _, closed, _ := r.state(); !closed {
		t.Error("runnable must be Closed even on the lock-contention path")
	}
}

// ---- config -----------------------------------------------------------------

func TestNewNilRunnable(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrNilRunnable) {
		t.Errorf("want ErrNilRunnable, got %v", err)
	}
}

func TestNewDefaultsName(t *testing.T) {
	svc, err := New(Config{Runnable: &fakeRunnable{runFn: func(context.Context) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc // name defaulted to DefaultName internally; New must not error
	_ = time.Now
}
