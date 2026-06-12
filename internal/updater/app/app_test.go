package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/updater/healthgate"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/pkg/hashing"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// ---- fakes ------------------------------------------------------------------

type fakeChecker struct {
	dec *manifestcheck.Decision
	err error
}

func (f *fakeChecker) Check(_ context.Context, _ manifest.Version) (*manifestcheck.Decision, error) {
	return f.dec, f.err
}

type fakeSwap struct {
	stageErr, backupErr, swapErr, restoreErr, commitErr error
	stage, backup, swap, restore, commit                int
	digest                                              hashing.Digest
	log                                                 *[]string
}

func (f *fakeSwap) Stage(_ context.Context, _ manifest.Artifact, _ string) error {
	f.stage++
	*f.log = append(*f.log, "stage")
	return f.stageErr
}
func (f *fakeSwap) Backup() (hashing.Digest, error) {
	f.backup++
	*f.log = append(*f.log, "backup")
	return f.digest, f.backupErr
}
func (f *fakeSwap) Swap() error {
	f.swap++
	*f.log = append(*f.log, "swap")
	return f.swapErr
}
func (f *fakeSwap) Restore(_ hashing.Digest) error {
	f.restore++
	*f.log = append(*f.log, "restore")
	return f.restoreErr
}
func (f *fakeSwap) Commit() error { f.commit++; *f.log = append(*f.log, "commit"); return f.commitErr }

type fakeGate struct{ res healthgate.Result }

func (f fakeGate) Wait(_ context.Context, _ healthgate.Expectation) healthgate.Result { return f.res }

type fakeSvc struct {
	stop, start, running int
	runningVal           bool
	stopErr, startErr    error
	log                  *[]string
}

func (f *fakeSvc) Stop() error {
	f.stop++
	*f.log = append(*f.log, "stop")
	return f.stopErr
}
func (f *fakeSvc) Start() error {
	f.start++
	*f.log = append(*f.log, "start")
	return f.startErr
}
func (f *fakeSvc) Running() (bool, error) { f.running++; return f.runningVal, nil }

type fakeMarker struct {
	writes   []string
	clears   int
	writeErr error
	log      *[]string
}

func (f *fakeMarker) Write(id string) error {
	f.writes = append(f.writes, id)
	*f.log = append(*f.log, "marker")
	return f.writeErr
}
func (f *fakeMarker) Clear() error { f.clears++; *f.log = append(*f.log, "clear"); return nil }

type auditEvent struct {
	eventType, outcome string
}
type fakeAudit struct{ events []auditEvent }

func (f *fakeAudit) Emit(eventType, outcome string, _ map[string]any) {
	f.events = append(f.events, auditEvent{eventType, outcome})
}
func (f *fakeAudit) has(eventType string) bool {
	for _, e := range f.events {
		if e.eventType == eventType {
			return true
		}
	}
	return false
}

// ---- harness ----------------------------------------------------------------

type harness struct {
	u      *Updater
	store  *StateStore
	check  *fakeChecker
	swap   *fakeSwap
	gate   *fakeGate
	svc    *fakeSvc
	marker *fakeMarker
	audit  *fakeAudit
	log    []string
}

func digest(t *testing.T) hashing.Digest {
	t.Helper()
	d, err := hashing.ParseDigest("blake3:" + strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func decision(current, target, releasedAt string) *manifestcheck.Decision {
	cv, _ := manifest.ParseVersion(current)
	tv, _ := manifest.ParseVersion(target)
	return &manifestcheck.Decision{
		Proceed: true, Reason: manifestcheck.ReasonOK,
		Manifest:       &manifest.Manifest{KeyID: "k1", ReleasedAt: releasedAt},
		Artifact:       manifest.Artifact{Platform: "linux", Arch: "amd64", URL: "https://x/a"},
		CurrentVersion: cv, TargetVersion: tv,
	}
}

func newHarness(t *testing.T, dec *manifestcheck.Decision, gate healthgate.Result) *harness {
	t.Helper()
	h := &harness{}
	h.store = NewStateStore(t.TempDir())
	h.check = &fakeChecker{dec: dec}
	h.swap = &fakeSwap{digest: digest(t), log: &h.log}
	h.gate = &fakeGate{res: gate}
	h.svc = &fakeSvc{log: &h.log}
	h.marker = &fakeMarker{log: &h.log}
	h.audit = &fakeAudit{}
	bv, _ := manifest.ParseVersion("1.0.0")
	u, err := New(Deps{
		Store: h.store, Check: h.check, Swap: h.swap, Gate: h.gate,
		Service: h.svc, Marker: h.marker, Audit: h.audit,
		BuildVersion: bv, TargetOS: "linux", Arch: "amd64",
		HealthWindow: 90 * time.Second,
		Now:          func() time.Time { return time.Unix(1700000000, 0).UTC() },
		MintID:       func() (string, error) { return "upd_test", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.u = u
	return h
}

func healthy() healthgate.Result {
	return healthgate.Result{Healthy: true, Reason: healthgate.ReasonOK}
}
func unhealthy() healthgate.Result { return healthgate.Result{Reason: healthgate.ReasonTimeout} }

// ---- happy path -------------------------------------------------------------

func TestApplyHappyPath(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	out, err := h.u.Apply(context.Background())
	if err != nil || out != OutcomeUpdated {
		t.Fatalf("apply = %s, %v; want updated", out, err)
	}
	want := []string{"stage", "stop", "backup", "swap", "marker", "start", "commit", "clear"}
	if strings.Join(h.log, ",") != strings.Join(want, ",") {
		t.Errorf("sequence = %v, want %v", h.log, want)
	}
	st, _ := h.store.Load()
	if st.FSMState != StateIdle || st.CurrentVersion != "1.2.0" {
		t.Errorf("post-commit state = %+v, want IDLE@1.2.0", st)
	}
	if st.LastSeenReleasedAt != "2026-06-10T00:00:00Z" {
		t.Errorf("watermark = %q, want raised", st.LastSeenReleasedAt)
	}
	for _, ev := range []string{evOffered, evManifestVerified, evStaged, evSwapped, evHealthOK, evSucceeded} {
		if !h.audit.has(ev) {
			t.Errorf("missing audit event %s", ev)
		}
	}
	if h.marker.writes[0] != "upd_test" {
		t.Errorf("marker update_id = %q", h.marker.writes[0])
	}
}

// marker must be written BEFORE the agent is started.
func TestMarkerWrittenBeforeStart(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	_, _ = h.u.Apply(context.Background())
	mi, si := indexOf(h.log, "marker"), indexOf(h.log, "start")
	if mi < 0 || si < 0 || mi > si {
		t.Errorf("marker(%d) must precede start(%d): %v", mi, si, h.log)
	}
}

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
