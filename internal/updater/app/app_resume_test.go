package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func saveState(t *testing.T, h *harness, mutate func(*State)) {
	t.Helper()
	s := IdleState()
	mutate(s)
	if err := h.store.Save(s); err != nil {
		t.Fatal(err)
	}
}

// deadline string in the future relative to the fake clock (gate is faked anyway).
func futureDeadline() string {
	return time.Unix(1700000000, 0).UTC().Add(90 * time.Second).Format(time.RFC3339)
}

// ---- rollback ---------------------------------------------------------------

func TestApplyRollbackOnUnhealthy(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), unhealthy())
	out, err := h.u.Apply(context.Background())
	if out != OutcomeRolledBack || err != nil {
		t.Fatalf("out=%s err=%v; want rolled_back", out, err)
	}
	if h.swap.restore != 1 {
		t.Errorf("restore calls = %d, want 1", h.swap.restore)
	}
	if !h.audit.has(evRolledBack) {
		t.Error("missing update.rolled_back")
	}
	st, _ := h.store.Load()
	if st.FSMState != StateIdle || st.LastFailedTarget != "1.2.0" {
		t.Errorf("post-rollback = %+v; want IDLE, quarantine 1.2.0", st)
	}
	if st.CurrentVersion == "1.2.0" {
		t.Error("current_version must NOT be raised on rollback")
	}
	if h.marker.clears == 0 {
		t.Error("marker/health must be cleared on rollback")
	}
}

func TestRollbackRestoreFailure(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), unhealthy())
	h.swap.restoreErr = swap.ErrBackupCorrupt
	out, err := h.u.Apply(context.Background())
	if out != OutcomeRollbackFailed || !errors.Is(err, ErrRollbackFailed) {
		t.Fatalf("out=%s err=%v; want rollback_failed", out, err)
	}
	st, _ := h.store.Load()
	if st.FSMState != StateRollingBack {
		t.Errorf("state must stay ROLLING_BACK for retry, got %s", st.FSMState)
	}
}

// ---- released_at monotonicity (FI-T24-1) ------------------------------------

func TestReleasedAtMonotonicity(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-01T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.CurrentVersion = "1.1.0"; s.LastSeenReleasedAt = "2026-06-15T00:00:00Z" }) // already updated
	out, err := h.u.Apply(context.Background())
	if out != OutcomeRejected || !errors.Is(err, ErrStaleManifest) {
		t.Fatalf("out=%s err=%v; want rejected/stale", out, err)
	}
	if h.swap.stage != 0 {
		t.Error("a stale manifest must not be staged")
	}
}

func TestReleasedAtRequiredAfterFirst(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", ""), healthy()) // absent released_at
	saveState(t, h, func(s *State) { s.CurrentVersion = "1.1.0"; s.LastSeenReleasedAt = "2026-06-15T00:00:00Z" })
	if out, err := h.u.Apply(context.Background()); out != OutcomeRejected || !errors.Is(err, ErrStaleManifest) {
		t.Fatalf("absent released_at after first update: out=%s err=%v", out, err)
	}
}

func TestReleasedAtAbsentAllowedFirstUpdate(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", ""), healthy()) // absent, no watermark yet
	if out, err := h.u.Apply(context.Background()); out != OutcomeUpdated || err != nil {
		t.Fatalf("absent released_at on first update should be allowed: out=%s err=%v", out, err)
	}
}

// ---- quarantine + attempt cap ----------------------------------------------

func TestQuarantineBlocksReapply(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.LastFailedTarget = "1.2.0" })
	if out, err := h.u.Apply(context.Background()); out != OutcomeRejected || !errors.Is(err, ErrQuarantined) {
		t.Fatalf("re-apply of quarantined target: out=%s err=%v", out, err)
	}
	if h.swap.stage != 0 {
		t.Error("quarantined target must not be staged")
	}
}

func TestStrictlyNewerClearsQuarantine(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.3.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.LastFailedTarget = "1.2.0" }) // 1.3.0 > 1.2.0
	if out, err := h.u.Apply(context.Background()); out != OutcomeUpdated || err != nil {
		t.Fatalf("strictly newer target should proceed: out=%s err=%v", out, err)
	}
}

func TestAttemptCap(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.AttemptTarget = "1.2.0"; s.Attempt = 2 }) // next attempt = 3 > 2
	if out, err := h.u.Apply(context.Background()); out != OutcomeRejected || !errors.Is(err, ErrAttemptCap) {
		t.Fatalf("attempt cap: out=%s err=%v", out, err)
	}
	st, _ := h.store.Load()
	if st.LastFailedTarget != "1.2.0" {
		t.Error("exceeding the attempt cap must quarantine the target")
	}
}

// ---- crash recovery: pre-swap abort-safe ------------------------------------

func TestResumePreSwapAbortSafe(t *testing.T) {
	for _, state := range []string{StateManifestVerified, StateStaged, StateStoppingAgent, StateBackedUp} {
		t.Run(state, func(t *testing.T) {
			h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
			saveState(t, h, func(s *State) {
				s.FSMState = state
				s.TargetVersion, s.UpdateID, s.Artifact = "1.2.0", "upd_x", manifest.Artifact{Platform: "linux"}
			})
			out, err := h.u.Apply(context.Background())
			if out != OutcomeRecovered || err != nil {
				t.Fatalf("resume %s: out=%s err=%v; want recovered", state, out, err)
			}
			if h.swap.swap != 0 || h.swap.backup != 0 || h.swap.restore != 0 {
				t.Errorf("pre-swap abort must not swap/backup/restore: %+v", h.swap)
			}
			if h.svc.start == 0 {
				t.Error("pre-swap abort must ensure the agent is started")
			}
			st, _ := h.store.Load()
			if st.FSMState != StateIdle {
				t.Errorf("post-abort state = %s, want IDLE", st.FSMState)
			}
		})
	}
}

// ---- crash recovery: post-swap resume forward -------------------------------

func TestResumeHealthCheckCommits(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateHealthCheck
		s.TargetVersion, s.UpdateID, s.HealthDeadline = "1.2.0", "upd_x", futureDeadline()
		s.BackupDigest = digest(t).String()
		s.PendingReleasedAt = "2026-06-10T00:00:00Z"
	})
	out, err := h.u.Apply(context.Background())
	if out != OutcomeUpdated || err != nil {
		t.Fatalf("resume HEALTH_CHECK healthy: out=%s err=%v", out, err)
	}
	if h.swap.swap != 0 || h.swap.backup != 0 {
		t.Errorf("resume must NOT re-swap/re-backup: %+v", h.swap)
	}
	st, _ := h.store.Load()
	if st.CurrentVersion != "1.2.0" || st.LastSeenReleasedAt != "2026-06-10T00:00:00Z" {
		t.Errorf("resume commit must raise version+watermark: %+v", st)
	}
}

func TestResumeHealthCheckRollsBack(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), unhealthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateHealthCheck
		s.TargetVersion, s.UpdateID, s.HealthDeadline = "1.2.0", "upd_x", futureDeadline()
		s.BackupDigest = digest(t).String()
	})
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeRolledBack {
		t.Fatalf("resume HEALTH_CHECK unhealthy: out=%s; want rolled_back", out)
	}
	if h.swap.restore != 1 || h.swap.swap != 0 || h.swap.backup != 0 {
		t.Errorf("resume rollback: restore once, no swap/backup: %+v", h.swap)
	}
}

func TestResumeStartingAgentNeverReSwaps(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateStartingAgent
		s.TargetVersion, s.UpdateID = "1.2.0", "upd_x"
		s.BackupDigest = digest(t).String()
	})
	if _, err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.swap.swap != 0 || h.swap.backup != 0 {
		t.Errorf("STARTING_AGENT resume must not swap/backup: %+v", h.swap)
	}
}

func TestResumeSwappingDisambiguatesOnce(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateSwapping
		s.TargetVersion, s.UpdateID = "1.2.0", "upd_x"
		s.BackupDigest = digest(t).String()
	})
	if _, err := h.u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.swap.swap != 1 { // exactly one disambiguation attempt
		t.Errorf("SWAPPING resume swap calls = %d, want 1", h.swap.swap)
	}
	if h.swap.backup != 0 {
		t.Error("SWAPPING resume must not re-backup")
	}
}

func TestResumeSwappingErrNotStagedMeansDone(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.swap.swapErr = swap.ErrNotStaged // already swapped before the crash
	saveState(t, h, func(s *State) {
		s.FSMState = StateSwapping
		s.TargetVersion, s.UpdateID = "1.2.0", "upd_x"
		s.BackupDigest = digest(t).String()
	})
	out, err := h.u.Apply(context.Background())
	if out != OutcomeUpdated || err != nil {
		t.Fatalf("ErrNotStaged on resume means already swapped -> forward: out=%s err=%v", out, err)
	}
}

func TestResumeRollingBackRetries(t *testing.T) {
	h := newHarness(t, decision("1.1.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateRollingBack
		s.TargetVersion = "1.2.0"
		s.BackupDigest = digest(t).String()
	})
	out, _ := h.u.Apply(context.Background())
	if out != OutcomeRolledBack {
		t.Fatalf("resume ROLLING_BACK: out=%s; want rolled_back", out)
	}
	if h.swap.restore != 1 {
		t.Errorf("resume rollback restore calls = %d, want 1", h.swap.restore)
	}
}

// ---- update_id ---------------------------------------------------------------

func TestMintUpdateIDEntropy(t *testing.T) {
	id1, err := mintUpdateID()
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := mintUpdateID()
	if id1 == id2 {
		t.Error("two minted ids collided")
	}
	if !strings.HasPrefix(id1, "upd_") || len(id1) != len("upd_")+32 { // 16 bytes -> 32 hex
		t.Errorf("update_id = %q, want upd_<32 hex> (128-bit)", id1)
	}
}

// ---- HIGH regression: first-update absent-released_at must not re-open the bypass

func TestFirstUpdateAbsentClosesReplayBypass(t *testing.T) {
	h := newHarness(t, decision("1.0.0", "1.2.0", ""), healthy()) // first update, absent released_at
	if out, err := h.u.Apply(context.Background()); out != OutcomeUpdated || err != nil {
		t.Fatalf("first absent-released_at update: out=%s err=%v", out, err)
	}
	st, _ := h.store.Load()
	if st.CurrentVersion != "1.2.0" {
		t.Fatalf("first update should set current_version, got %q", st.CurrentVersion)
	}
	// A SECOND update with absent released_at must now be REJECTED — the first-update
	// leniency is one-time (gated on current_version), not re-triggerable.
	h.check.dec = decision("1.2.0", "1.3.0", "")
	if out, err := h.u.Apply(context.Background()); out != OutcomeRejected || !errors.Is(err, ErrStaleManifest) {
		t.Errorf("second absent-released_at must be rejected: out=%s err=%v", out, err)
	}
}

// abortPreSwap (on a forward pre-swap failure) must leave NO in-flight state at IDLE
// but KEEP the attempt counter, and must clear any marker.
func TestAbortPreSwapClearsInFlightKeepsAttempt(t *testing.T) {
	h := newHarness(t, decision("1.0.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	h.swap.stageErr = errors.New("stage boom")
	if _, _ = h.u.Apply(context.Background()); h.marker.clears == 0 {
		t.Error("abort must clear the marker")
	}
	st, _ := h.store.Load()
	if st.FSMState != StateIdle || st.TargetVersion != "" || st.PendingReleasedAt != "" || st.UpdateID != "" {
		t.Errorf("abort must clear in-flight fields: %+v", st)
	}
	if st.AttemptTarget != "1.2.0" || st.Attempt != 1 {
		t.Errorf("abort must KEEP the attempt counter: target=%q attempt=%d", st.AttemptTarget, st.Attempt)
	}
}
