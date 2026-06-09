package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/heartbeat"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/agent/tasks"
)

var dummyPin = "sha256:" + strings.Repeat("ab", 32)

// ---- fake builder (flow tests) ----------------------------------------------

type loopFn func(ctx context.Context) error

func (f loopFn) Run(ctx context.Context) error { return f(ctx) }

type fakeBuilder struct {
	enrolled    bool
	enrolledErr error
	enrollErr   error
	enrollCalls int

	hbRun      func(ctx context.Context, wa chan<- struct{}) error
	tkRun      func(ctx context.Context, poke <-chan struct{}) error
	hbBuildErr error
	tkBuildErr error
	hbStarted  chan struct{}
	tkStarted  chan struct{}
	onEnroll   func() // hook invoked inside Enroll (e.g. to cancel mid-enroll)
}

func (f *fakeBuilder) IsEnrolled() (bool, error) { return f.enrolled, f.enrolledErr }

func (f *fakeBuilder) Enroll(_ context.Context) error {
	f.enrollCalls++
	if f.onEnroll != nil {
		f.onEnroll()
	}
	if f.enrollErr == nil {
		f.enrolled = true // enrollment succeeded -> now enrolled
	}
	return f.enrollErr
}

func (f *fakeBuilder) Heartbeat(wa chan<- struct{}) (loopRunner, error) {
	if f.hbBuildErr != nil {
		return nil, f.hbBuildErr
	}
	return loopFn(func(ctx context.Context) error {
		if f.hbStarted != nil {
			close(f.hbStarted)
		}
		return f.hbRun(ctx, wa)
	}), nil
}

func (f *fakeBuilder) Tasks(poke <-chan struct{}) (loopRunner, error) {
	if f.tkBuildErr != nil {
		return nil, f.tkBuildErr
	}
	return loopFn(func(ctx context.Context) error {
		if f.tkStarted != nil {
			close(f.tkStarted)
		}
		return f.tkRun(ctx, poke)
	}), nil
}

func appWith(b builder) *App { return &App{b: b} } // log nil -> logInfo is a no-op

func blockUntilCtx(ctx context.Context, _ chan<- struct{}) error   { <-ctx.Done(); return ctx.Err() }
func blockUntilCtxTk(ctx context.Context, _ <-chan struct{}) error { <-ctx.Done(); return ctx.Err() }

// ---- run-flow tests ---------------------------------------------------------

func TestRunEnrolledPathStartsLoops(t *testing.T) {
	hbStarted, tkStarted := make(chan struct{}), make(chan struct{})
	fb := &fakeBuilder{
		enrolled: true, hbStarted: hbStarted, tkStarted: tkStarted,
		hbRun: blockUntilCtx, tkRun: blockUntilCtxTk,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- appWith(fb).Run(ctx) }()

	waitClosed(t, hbStarted, "heartbeat")
	waitClosed(t, tkStarted, "tasks")
	cancel()
	if err := <-done; err != nil {
		t.Errorf("graceful shutdown should return nil, got %v", err)
	}
	if fb.enrollCalls != 0 {
		t.Errorf("an enrolled device must not re-enroll, calls=%d", fb.enrollCalls)
	}
}

func TestRunNotEnrolledTriggersEnrollment(t *testing.T) {
	hbStarted := make(chan struct{})
	fb := &fakeBuilder{
		enrolled: false, hbStarted: hbStarted,
		hbRun: blockUntilCtx, tkRun: blockUntilCtxTk,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- appWith(fb).Run(ctx) }()

	waitClosed(t, hbStarted, "heartbeat") // loops only start after enrollment
	cancel()
	<-done
	if fb.enrollCalls != 1 {
		t.Errorf("not-enrolled must enroll exactly once, calls=%d", fb.enrollCalls)
	}
}

func TestRunEnrollFailureSurfaces(t *testing.T) {
	fb := &fakeBuilder{enrolled: false, enrollErr: enroll.ErrTokenRejected}
	if err := appWith(fb).Run(context.Background()); !errors.Is(err, ErrEnrollFailed) {
		t.Errorf("err = %v, want ErrEnrollFailed", err)
	}
}

func TestRunEnroll426Surfaces(t *testing.T) {
	fb := &fakeBuilder{enrolled: false, enrollErr: enroll.ErrUpgradeRequired}
	if err := appWith(fb).Run(context.Background()); !errors.Is(err, ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
}

// A shutdown signal arriving during the startup enroll is graceful (nil), not a
// hard ErrEnrollFailed — the enroll error chain is flattened, so Run checks ctx.
func TestRunEnrollCancellationIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fb := &fakeBuilder{
		enrolled:  false,
		onEnroll:  cancel,                 // shutdown lands while enrolling
		enrollErr: enroll.ErrEnrollFailed, // flattened request-cancelled error (ctx not matchable)
	}
	if err := appWith(fb).Run(ctx); err != nil {
		t.Errorf("ctx-cancelled startup enroll must be graceful (nil), got %v", err)
	}
}

func TestRun401StopsRuntime(t *testing.T) {
	fb := &fakeBuilder{
		enrolled: true,
		hbRun:    func(context.Context, chan<- struct{}) error { return heartbeat.ErrUnauthorized },
		tkRun:    blockUntilCtxTk,
	}
	if err := appWith(fb).Run(context.Background()); !errors.Is(err, ErrEnrollmentRequired) {
		t.Errorf("err = %v, want ErrEnrollmentRequired", err)
	}
}

func TestRun426StopsRuntime(t *testing.T) {
	fb := &fakeBuilder{
		enrolled: true,
		hbRun:    blockUntilCtx,
		tkRun:    func(context.Context, <-chan struct{}) error { return tasks.ErrUpgradeRequired },
	}
	if err := appWith(fb).Run(context.Background()); !errors.Is(err, ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
}

func TestRunWorkAvailablePokesTasks(t *testing.T) {
	pokes := make(chan struct{}, 8)
	fb := &fakeBuilder{
		enrolled: true,
		hbRun: func(ctx context.Context, wa chan<- struct{}) error {
			select { // heartbeat nudges the task loop
			case wa <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		},
		tkRun: func(ctx context.Context, poke <-chan struct{}) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-poke:
					pokes <- struct{}{}
				}
			}
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- appWith(fb).Run(ctx) }()
	select {
	case <-pokes: // the task loop received the heartbeat's work_available poke
	case <-time.After(2 * time.Second):
		t.Error("task loop never received the work_available poke")
	}
	cancel()
	<-done
}

func TestRunHeartbeatBuildFailure(t *testing.T) {
	fb := &fakeBuilder{enrolled: true, hbBuildErr: errors.New("boom")}
	if err := appWith(fb).Run(context.Background()); !errors.Is(err, ErrRuntimeInit) {
		t.Errorf("err = %v, want ErrRuntimeInit", err)
	}
}

// ---- enrolled predicate (real store) ----------------------------------------

func TestEnrolledPredicate(t *testing.T) {
	st := openTestStore(t)
	if ok, err := enrolled(st); ok || err != nil {
		t.Errorf("empty store: ok=%v err=%v, want false/nil", ok, err)
	}
	if err := st.Put(state.KeyDeviceID, []byte("dev_1")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := enrolled(st); ok {
		t.Error("device_id alone must NEVER mean enrolled")
	}
	if err := st.Put(state.KeyCertificate, []byte("cert")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := enrolled(st); ok {
		t.Error("missing session token must not be enrolled")
	}
	if err := st.PutSecret(state.SecretSessionToken, []byte("ast_1")); err != nil {
		t.Fatal(err)
	}
	if ok, err := enrolled(st); err != nil || !ok {
		t.Errorf("all three present -> enrolled, got ok=%v err=%v", ok, err)
	}
}

// ---- New(): token seeding + init failures -----------------------------------

func TestNewSeedsTokenFromState(t *testing.T) {
	dir := t.TempDir()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	// Pre-populate a persisted session token, then close (same protector instance
	// keeps the wrap key so New() can unwrap on reopen).
	st, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutSecret(state.SecretSessionToken, []byte("ast_seed0001")); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	app, err := New(Options{Config: testConfig(t), StateDir: dir, Protector: prot, BootstrapPins: []string{dummyPin}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()
	if got := app.b.(*prodBuilder).client.SessionToken(); got != "ast_seed0001" {
		t.Errorf("token not seeded into saasclient: %q", got)
	}
}

func TestNewInitFailures(t *testing.T) {
	t.Run("config load/validate fail", func(t *testing.T) {
		_, err := New(Options{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"), BootstrapPins: []string{dummyPin}})
		if !errors.Is(err, ErrConfig) {
			t.Errorf("err = %v, want ErrConfig", err)
		}
	})
	t.Run("no SPKI bootstrap pins", func(t *testing.T) {
		prot, _ := state.NewInsecureTestProtector()
		_, err := New(Options{Config: testConfig(t), StateDir: t.TempDir(), Protector: prot, BootstrapPins: nil})
		if !errors.Is(err, ErrTransportInit) {
			t.Errorf("err = %v, want ErrTransportInit", err)
		}
	})
	t.Run("state init fail (dir path is a file)", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		prot, _ := state.NewInsecureTestProtector()
		_, err := New(Options{Config: testConfig(t), StateDir: filepath.Join(file, "state"), Protector: prot, BootstrapPins: []string{dummyPin}})
		if !errors.Is(err, ErrStateInit) {
			t.Errorf("err = %v, want ErrStateInit", err)
		}
	})
}

// TestProdBuilderWiresLoops exercises the REAL prodBuilder building the runtime
// loops from enrolled state, and that heartbeat+tasks share one audit emitter.
func TestProdBuilderWiresLoops(t *testing.T) {
	dir := t.TempDir()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		state.KeyDeviceID:    "dev_app1",
		state.KeyCertificate: "-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n",
		state.KeyTenantID:    "tnt_app1",
	} {
		if err := st.Put(k, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutSecret(state.SecretSessionToken, []byte("ast_app0001")); err != nil {
		t.Fatal(err)
	}
	_ = st.Close() // release the bbolt handle before New() reopens the same dir

	app, err := New(Options{Config: testConfig(t), StateDir: dir, Protector: prot, BootstrapPins: []string{dummyPin}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer app.Close()

	pb := app.b.(*prodBuilder)
	if ok, err := pb.IsEnrolled(); err != nil || !ok {
		t.Fatalf("expected enrolled, ok=%v err=%v", ok, err)
	}
	poke := make(chan struct{}, 1)
	hb, err := pb.Heartbeat(poke)
	if err != nil || hb == nil {
		t.Fatalf("Heartbeat build: %v", err)
	}
	tk, err := pb.Tasks(poke)
	if err != nil || tk == nil {
		t.Fatalf("Tasks build: %v", err)
	}
	if pb.runtimeEm == nil {
		t.Error("runtime emitter not built/shared between heartbeat and tasks")
	}
}

// ---- helpers ----------------------------------------------------------------

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	c := config.DefaultConfig()
	c.General.APIBaseURL = "https://api.example.test"
	c.Logging.FilePath = filepath.Join(t.TempDir(), "agent.log")
	c.Logging.Format = "json"
	c.Logging.Level = "info"
	return c
}

func openTestStore(t *testing.T) *state.Store {
	t.Helper()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(state.Options{Dir: t.TempDir(), Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func waitClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s loop did not start", name)
	}
}
