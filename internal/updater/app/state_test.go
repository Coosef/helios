package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStateAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	// Absent -> fresh IDLE.
	s, err := store.Load()
	if err != nil {
		t.Fatalf("Load(absent): %v", err)
	}
	if s.FSMState != StateIdle {
		t.Errorf("absent load = %q, want IDLE", s.FSMState)
	}

	s.FSMState = StateHealthCheck
	s.CurrentVersion = "1.3.0"
	s.TargetVersion = "1.4.0"
	s.UpdateID = "upd_abc"
	s.BackupDigest = "blake3:dead"
	s.LastSeenReleasedAt = "2026-06-01T00:00:00Z"
	s.Attempt = 1
	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// no temp leak
	if _, err := os.Stat(filepath.Join(dir, stateFileName+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp file leaked")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.FSMState != StateHealthCheck || got.TargetVersion != "1.4.0" || got.UpdateID != "upd_abc" ||
		got.BackupDigest != "blake3:dead" || got.Attempt != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestStateRejectsCorrupt(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)
	p := filepath.Join(dir, stateFileName)

	_ = os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := store.Load(); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("bad json: %v, want ErrStateCorrupt", err)
	}
	_ = os.WriteFile(p, []byte(`{"schema_version":1,"fsm_state":"BOGUS"}`), 0o600)
	if _, err := store.Load(); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("bad fsm_state: %v, want ErrStateCorrupt", err)
	}
	_ = os.WriteFile(p, []byte(`{"schema_version":2,"fsm_state":"IDLE"}`), 0o600)
	if _, err := store.Load(); !errors.Is(err, ErrStateCorrupt) {
		t.Errorf("schema bump: %v, want ErrStateCorrupt", err)
	}
}

func TestSaveRefusesUnknownState(t *testing.T) {
	store := NewStateStore(t.TempDir())
	if err := store.Save(&State{FSMState: "WAT"}); !errors.Is(err, ErrState) {
		t.Errorf("save unknown state: %v, want ErrState", err)
	}
}

func TestPreSwapClassification(t *testing.T) {
	pre := []string{StateManifestVerified, StateStaged, StateStoppingAgent, StateBackedUp}
	post := []string{StateSwapping, StateStartingAgent, StateHealthCheck, StateRollingBack}
	for _, s := range pre {
		if !isPreSwap(s) {
			t.Errorf("%s should be pre-swap", s)
		}
	}
	for _, s := range post {
		if isPreSwap(s) {
			t.Errorf("%s should NOT be pre-swap", s)
		}
	}
}
