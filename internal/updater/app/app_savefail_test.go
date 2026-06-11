package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A failed state Save at the attempt-cap quarantine must FAIL CLOSED (OutcomeError),
// never silently proceed (which would lose the quarantine and bypass the cap).
func TestAttemptCapSaveFailureFailsClosed(t *testing.T) {
	h := newHarness(t, decision("1.0.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) { s.AttemptTarget = "1.2.0"; s.Attempt = 2 })
	dir := filepath.Dir(h.store.path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("cannot chmod")
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck
	out, err := h.u.Apply(context.Background())
	if out != OutcomeError || err == nil {
		t.Errorf("cap quarantine save failure must fail closed: out=%s err=%v", out, err)
	}
}

// A failed state Save during pre-swap crash recovery must FAIL CLOSED, not be hidden
// behind OutcomeRecovered.
func TestRecoverPreSwapSaveFailureFailsClosed(t *testing.T) {
	h := newHarness(t, decision("1.0.0", "1.2.0", "2026-06-10T00:00:00Z"), healthy())
	saveState(t, h, func(s *State) {
		s.FSMState = StateBackedUp
		s.TargetVersion, s.UpdateID = "1.2.0", "upd_x"
	})
	dir := filepath.Dir(h.store.path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("cannot chmod")
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck
	out, err := h.u.Apply(context.Background())
	if out != OutcomeError || err == nil {
		t.Errorf("recover pre-swap save failure must fail closed: out=%s err=%v", out, err)
	}
}
