package app

import (
	"context"
	"errors"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func rejectDecision(reason, current, target string) *manifestcheck.Decision {
	cv, _ := manifest.ParseVersion(current)
	tv, _ := manifest.ParseVersion(target)
	return &manifestcheck.Decision{Proceed: false, Reason: reason, CurrentVersion: cv, TargetVersion: tv,
		Manifest: &manifest.Manifest{}}
}

// ---- Check (read-only) ------------------------------------------------------

func TestCheckReadOnly(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	dec, err := h.u.Check(context.Background())
	if err != nil || dec == nil || !dec.Proceed {
		t.Fatalf("check proceed: %+v %v", dec, err)
	}
	if h.swap.stage != 0 || h.svc.stop != 0 {
		t.Error("check must have NO side effects")
	}
	st, _ := h.store.Load()
	if st.FSMState != StateIdle {
		t.Error("check must not mutate persisted state")
	}
}

func TestCheckSurfacesStale(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-01T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.CurrentVersion = "1.1.0"; s.LastSeenReleasedAt = "2026-06-15T00:00:00Z" })
	dec, err := h.u.Check(context.Background())
	if !errors.Is(err, ErrStaleManifest) || dec.Proceed {
		t.Errorf("check should surface stale: dec=%+v err=%v", dec, err)
	}
}

// ---- rejection mapping ------------------------------------------------------

func TestRejectionUpToDateNoNoise(t *testing.T) {
	// target == current -> benign "already up to date", no audit noise.
	h := newHarness(t, rejectDecision(manifestcheck.ReasonDowngradeBlocked, "1.2.0", "1.2.0"), healthy())
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeNoUpdate {
		t.Errorf("up-to-date: out=%s, want no_update", out)
	}
	if h.audit.has(evDowngradeBlocked) {
		t.Error("up-to-date must not emit downgrade_blocked")
	}
}

func TestRejectionDowngradeAndSignature(t *testing.T) {
	h := newHarness(t, rejectDecision(manifestcheck.ReasonDowngradeBlocked, "1.5.0", "1.2.0"), healthy())
	if out, _ := h.u.Apply(context.Background()); out != OutcomeRejected || !h.audit.has(evDowngradeBlocked) {
		t.Errorf("downgrade attempt: out=%s events=%v", out, h.audit.events)
	}
	h2 := newHarness(t, rejectDecision(manifestcheck.ReasonManifestRejected, "1.1.0", "1.2.0"), healthy())
	if out, _ := h2.u.Apply(context.Background()); out != OutcomeRejected || !h2.audit.has(evSignatureInvalid) {
		t.Errorf("bad signature: out=%s events=%v", out, h2.audit.events)
	}
	h3 := newHarness(t, rejectDecision(manifestcheck.ReasonBelowFloor, "1.1.0", "1.2.0"), healthy())
	if out, _ := h3.u.Apply(context.Background()); out != OutcomeRejected || !h3.audit.has(evFailed) {
		t.Errorf("below floor: out=%s events=%v", out, h3.audit.events)
	}
}

// ---- pre-swap failure paths (abort, live binary untouched) ------------------

func TestStageFailureAborts(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.swap.stageErr = verify.ErrHashMismatch
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeError || h.swap.swap != 0 || !h.audit.has(evHashMismatch) {
		t.Errorf("stage hash mismatch: out=%s swap=%d events=%v", out, h.swap.swap, h.audit.events)
	}
	st, _ := h.store.Load()
	if st.FSMState != StateIdle {
		t.Errorf("post-abort state = %s, want IDLE", st.FSMState)
	}
}

func TestStopFailureAborts(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.svc.stopErr = errors.New("scm busy")
	if out, _ := h.u.Apply(context.Background()); out != OutcomeError || h.swap.backup != 0 {
		t.Errorf("stop failure must abort before backup: out=%s backup=%d", out, h.swap.backup)
	}
}

func TestBackupFailureAborts(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.swap.backupErr = errors.New("disk full")
	if out, _ := h.u.Apply(context.Background()); out != OutcomeError || h.swap.swap != 0 {
		t.Errorf("backup failure must abort before swap: out=%s swap=%d", out, h.swap.swap)
	}
}

func TestSwapFailureAbortsNotRollback(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.swap.swapErr = errors.New("rename EXDEV")
	out, _ := h.u.Apply(context.Background())
	// Atomic rename failed -> live binary is still old -> abort, NOT rollback.
	if out != OutcomeError || h.swap.restore != 0 {
		t.Errorf("swap failure must abort (no restore): out=%s restore=%d", out, h.swap.restore)
	}
	if h.svc.start == 0 {
		t.Error("swap-failure abort must restart the (old) agent")
	}
}

// ---- post-swap failure paths (rollback) -------------------------------------

func TestMarkerWriteFailureRollsBack(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.marker.writeErr = errors.New("acl denied")
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeRolledBack || h.swap.restore != 1 {
		t.Errorf("marker write failure (post-swap) must roll back: out=%s restore=%d", out, h.swap.restore)
	}
}

func TestStartFailureRollsBack(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.svc.startErr = errors.New("start failed")
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeRolledBack || h.swap.restore != 1 {
		t.Errorf("start failure (post-swap) must roll back: out=%s restore=%d", out, h.swap.restore)
	}
}

// ---- ProdMarker (real health files) -----------------------------------------

func TestProdMarkerWriteAndClear(t *testing.T) {
	dir := t.TempDir()
	m := ProdMarker{StateDir: dir}
	if err := m.Write("upd_real"); err != nil {
		t.Fatal(err)
	}
	if mk, err := health.ReadMarker(dir); err != nil || mk.UpdateID != "upd_real" {
		t.Fatalf("marker not written: %+v %v", mk, err)
	}
	_ = health.WriteHealth(dir, health.Record{UpdateID: "upd_real", Result: health.ResultOK, WrittenAt: "2026-01-02T03:04:05Z"})
	if err := m.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := health.ReadMarker(dir); !errors.Is(err, health.ErrAbsent) {
		t.Error("Clear must remove the marker")
	}
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Error("Clear must remove health.json")
	}
}

// ---- baseline / current edges -----------------------------------------------

func TestBaselineHighWater(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", ""), healthy())
	st := IdleState()
	st.CurrentVersion = "2.0.0" // persisted current > build (1.0.0)
	if got := h.u.baseline(st); got.String() != "2.0.0" {
		t.Errorf("baseline = %s, want 2.0.0 (persisted high-water)", got.String())
	}
	st.CurrentVersion = "" // unknown -> build version floor
	if got := h.u.baseline(st); got.String() != "1.0.0" {
		t.Errorf("baseline = %s, want build 1.0.0", got.String())
	}
	st.CurrentVersion = "not-a-version" // unparseable -> zero -> build wins
	if got := h.u.baseline(st); got.String() != "1.0.0" {
		t.Errorf("baseline with bad current = %s, want 1.0.0", got.String())
	}
}
