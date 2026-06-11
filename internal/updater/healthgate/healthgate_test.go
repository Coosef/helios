package healthgate_test

import (
	"context"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/internal/updater/healthgate"
)

// mockClock advances on each sleep so the 90s window is exercised instantly.
type mockClock struct{ t time.Time }

func (m *mockClock) now() time.Time { return m.t }
func (m *mockClock) sleep(_ context.Context, d time.Duration) error {
	m.t = m.t.Add(d)
	return nil
}

func writeHealth(t *testing.T, dir, updateID, result string, at time.Time) {
	t.Helper()
	if err := health.WriteHealth(dir, health.Record{UpdateID: updateID, Result: result, WrittenAt: at.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
}

func newGate(t *testing.T, dir string, running func() (bool, error), mc *mockClock) *healthgate.Gate {
	t.Helper()
	g, err := healthgate.New(dir, running, healthgate.WithPoll(time.Second), healthgate.WithClock(mc.now), healthgate.WithSleeper(mc.sleep))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func exp(mc *mockClock, updateID string) healthgate.Expectation {
	return healthgate.Expectation{UpdateID: updateID, GateStart: mc.t, Deadline: mc.t.Add(healthgate.DefaultWindow)}
}

func TestGateHealthy(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	writeHealth(t, dir, "upd_X", health.ResultOK, mc.t)
	g := newGate(t, dir, func() (bool, error) { return true, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if !r.Healthy || r.Reason != healthgate.ReasonOK {
		t.Errorf("healthy: %+v", r)
	}
}

func TestGateTimeoutNoHealth(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	g := newGate(t, dir, func() (bool, error) { return true, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if r.Healthy || r.Reason != healthgate.ReasonTimeout {
		t.Errorf("no health -> timeout, got %+v", r)
	}
}

func TestGateTimeoutServiceNotRunning(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	writeHealth(t, dir, "upd_X", health.ResultOK, mc.t) // health ok but service down
	g := newGate(t, dir, func() (bool, error) { return false, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if r.Healthy || r.Reason != healthgate.ReasonTimeout || r.Detail != healthgate.ReasonServiceNotRunning {
		t.Errorf("service down -> timeout/service_not_running, got %+v", r)
	}
}

func TestGateStaleUpdateID(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	writeHealth(t, dir, "upd_OLD", health.ResultOK, mc.t) // stale: prior update's id
	g := newGate(t, dir, func() (bool, error) { return true, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_NEW"))
	if r.Healthy || r.Reason != healthgate.ReasonTimeout {
		t.Errorf("stale update_id must not pass; got %+v", r)
	}
}

func TestGateHealthFailedIsImmediate(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	writeHealth(t, dir, "upd_X", health.ResultFailed, mc.t)
	g := newGate(t, dir, func() (bool, error) { return true, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if r.Healthy || r.Reason != healthgate.ReasonHealthFailed {
		t.Errorf("self-reported failure -> health_failed, got %+v", r)
	}
}

func TestGateWrittenAtOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	// written_at BEFORE the gate started (stale file with the right id but old time)
	writeHealth(t, dir, "upd_X", health.ResultOK, mc.t.Add(-time.Hour))
	g := newGate(t, dir, func() (bool, error) { return true, nil }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if r.Healthy {
		t.Errorf("written_at before gate start must not pass; got %+v", r)
	}
}

func TestGateContextCanceled(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	g := newGate(t, dir, func() (bool, error) { return false, nil }, mc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := g.Wait(ctx, exp(mc, "upd_X"))
	if r.Reason != healthgate.ReasonCanceled {
		t.Errorf("canceled: %+v", r)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := healthgate.New("", func() (bool, error) { return true, nil }); err == nil {
		t.Error("empty dir should error")
	}
	if _, err := healthgate.New("/x", nil); err == nil {
		t.Error("nil running should error")
	}
}

func TestGateServiceQueryErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClock{t: time.Now().UTC()}
	writeHealth(t, dir, "upd_X", health.ResultOK, mc.t) // health ok, but service query errors
	g := newGate(t, dir, func() (bool, error) { return false, errSCM }, mc)
	r := g.Wait(context.Background(), exp(mc, "upd_X"))
	if r.Healthy || r.Reason != healthgate.ReasonTimeout || r.Detail != healthgate.ReasonServiceNotRunning {
		t.Errorf("service query error must fail closed; got %+v", r)
	}
}

func TestGateInconsistentClockDoesNotSpin(t *testing.T) {
	dir := t.TempDir() // no health -> would poll; jump the clock past the deadline mid-iteration
	jc := &jumpClock{base: time.Now().UTC()}
	g, err := healthgate.New(dir, func() (bool, error) { return true, nil },
		healthgate.WithClock(jc.now), healthgate.WithSleeper(func(context.Context, time.Duration) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	r := g.Wait(context.Background(), healthgate.Expectation{UpdateID: "u", GateStart: jc.base, Deadline: jc.base.Add(healthgate.DefaultWindow)})
	if r.Reason != healthgate.ReasonTimeout {
		t.Errorf("inconsistent clock should time out cleanly; got %+v", r)
	}
}

type jumpClock struct {
	base  time.Time
	calls int
}

func (c *jumpClock) now() time.Time {
	c.calls++
	if c.calls <= 1 {
		return c.base // first check: before deadline
	}
	return c.base.Add(200 * time.Second) // later: jumped past deadline -> negative remaining
}

var errSCM = errSimple("scm unavailable")

type errSimple string

func (e errSimple) Error() string { return string(e) }
