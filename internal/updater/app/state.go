package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// FSM states (Technical Design §1.10). The string values are persisted in
// updater_state.json and must stay stable across versions.
const (
	StateIdle             = "IDLE"
	StateManifestVerified = "MANIFEST_VERIFIED"
	StateStaged           = "STAGED"
	StateStoppingAgent    = "STOPPING_AGENT"
	StateBackedUp         = "BACKED_UP"
	StateSwapping         = "SWAPPING"
	StateStartingAgent    = "STARTING_AGENT"
	StateHealthCheck      = "HEALTH_CHECK"
	StateRollingBack      = "ROLLING_BACK"
)

// stateSchemaVersion is the only updater_state.json schema this build understands.
const stateSchemaVersion = 1

// maxStateBytes bounds the state read (defense-in-depth). The record is < 4 KiB.
const maxStateBytes = 256 << 10

const stateFileName = "updater_state.json"

var (
	// ErrState wraps a failure to read/write updater_state.json.
	ErrState = errors.New("updater: state persistence failed")
	// ErrStateCorrupt is returned when the persisted state is malformed.
	ErrStateCorrupt = errors.New("updater: state file corrupt")
)

// State is the persisted updater FSM state (standalone atomic JSON file — NEVER
// the agent's bbolt store, which a separate process cannot share, §0.1).
type State struct {
	SchemaVersion int    `json:"schema_version"`
	FSMState      string `json:"fsm_state"`
	// CurrentVersion is the persisted high-water installed version (raised ONLY
	// after a passed health gate). The empty string means "unknown" (use buildinfo).
	CurrentVersion string `json:"current_version,omitempty"`
	// TargetVersion is the in-flight update target.
	TargetVersion string `json:"target_version,omitempty"`
	// UpdateID is the CSPRNG nonce for the in-flight attempt (echoed in health.json).
	UpdateID string `json:"update_id,omitempty"`
	// HealthDeadline is the RFC3339 end of the 90s gate window (persisted so a
	// resumed HEALTH_CHECK is deterministic).
	HealthDeadline string `json:"health_deadline,omitempty"`
	// BackupDigest is the recorded BLAKE3 of the .bak (the rollback integrity anchor).
	BackupDigest string `json:"backup_digest,omitempty"`
	// LastSeenReleasedAt is the released_at freshness high-water mark (FI-T24-1),
	// raised ONLY after a passed gate (atomically with current_version).
	LastSeenReleasedAt string `json:"last_seen_released_at,omitempty"`
	// PendingReleasedAt is the in-flight candidate released_at (resume metadata),
	// promoted to LastSeenReleasedAt at the commit point.
	PendingReleasedAt string `json:"pending_released_at,omitempty"`
	// LastFailedTarget is the quarantined version (the just-rolled-back target).
	LastFailedTarget string `json:"last_failed_target,omitempty"`
	// AttemptTarget is the target the Attempt counter refers to. It is decoupled
	// from the in-flight TargetVersion (which is cleared at IDLE) so the crash-loop
	// counter survives an abort without leaving an in-flight target set at IDLE.
	AttemptTarget string `json:"attempt_target,omitempty"`
	// Attempt counts consecutive apply attempts for AttemptTarget.
	Attempt int `json:"attempt,omitempty"`
	// Artifact is the selected artifact metadata, persisted so a resumed apply can
	// re-stage without re-fetching the manifest.
	Artifact manifest.Artifact `json:"artifact,omitempty"`
}

// IdleState returns a fresh IDLE state.
func IdleState() *State { return &State{SchemaVersion: stateSchemaVersion, FSMState: StateIdle} }

// current returns the persisted current_version as a Version (zero value if unset).
func (s *State) current() manifest.Version {
	if s.CurrentVersion == "" {
		return manifest.Version{}
	}
	v, err := manifest.ParseVersion(s.CurrentVersion)
	if err != nil {
		return manifest.Version{}
	}
	return v
}

// StateStore reads/writes updater_state.json atomically in the state directory.
type StateStore struct{ path string }

// NewStateStore binds a store to the state directory (the same ACL-locked dir the
// agent uses; the updater writes ONLY this file there, never the bbolt store).
func NewStateStore(stateDir string) *StateStore {
	return &StateStore{path: filepath.Join(stateDir, stateFileName)}
}

// Load reads the persisted state, returning a fresh IDLE state if none exists.
func (st *StateStore) Load() (*State, error) {
	f, err := os.Open(st.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return IdleState(), nil
		}
		return nil, fmt.Errorf("%w: open: %v", ErrState, err)
	}
	defer f.Close() //nolint:errcheck // read-only
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrState, err)
	}
	if len(b) > maxStateBytes {
		return nil, fmt.Errorf("%w: exceeds %d bytes", ErrStateCorrupt, maxStateBytes)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	if s.SchemaVersion != stateSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version %d", ErrStateCorrupt, s.SchemaVersion)
	}
	if !validState(s.FSMState) {
		return nil, fmt.Errorf("%w: unknown fsm_state %q", ErrStateCorrupt, s.FSMState)
	}
	return &s, nil
}

// Save persists the state atomically (write-temp → fsync → rename); the temp file
// is removed on any failure so a partial write can never become the live file.
func (st *StateStore) Save(s *State) error {
	s.SchemaVersion = stateSchemaVersion
	if !validState(s.FSMState) {
		return fmt.Errorf("%w: refusing to persist unknown fsm_state %q", ErrState, s.FSMState)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrState, err)
	}
	b = append(b, '\n')
	tmp := st.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create temp: %v", ErrState, err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: write: %v", ErrState, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: sync: %v", ErrState, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: close: %v", ErrState, err)
	}
	if err := os.Rename(tmp, st.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: rename: %v", ErrState, err)
	}
	return nil
}

func validState(s string) bool {
	switch s {
	case StateIdle, StateManifestVerified, StateStaged, StateStoppingAgent,
		StateBackedUp, StateSwapping, StateStartingAgent, StateHealthCheck, StateRollingBack:
		return true
	}
	return false
}

// isPreSwap reports whether a persisted state is provably before the atomic swap
// (the live binary is untouched → abort-safe on crash recovery).
func isPreSwap(s string) bool {
	switch s {
	case StateManifestVerified, StateStaged, StateStoppingAgent, StateBackedUp:
		return true
	}
	return false
}
