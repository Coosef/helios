// Package health defines the post-update health handshake between the agent and the
// on-demand updater (S1-T26): the update MARKER (updater → agent: the update_id the
// agent must echo) and the health RECORD (agent → updater: the self-report the
// updater's health gate validates). It is a neutral LEAF package (standard library
// only) imported by both sides, so neither the agent nor the updater depends on the
// other's package tree (no import cycle).
//
// Both files live in the agent's ACL-locked state directory. The MARKER's update_id
// (an updater-generated CSPRNG nonce) is the freshness anchor: a stale health.json
// from a PRIOR update carries a different update_id and therefore cannot satisfy a
// later gate. Files are written atomically (write-temp → fsync → rename).
package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Schema/contract constants.
const (
	// SchemaVersion is the only health/marker schema version this build understands.
	SchemaVersion = 1
	// ResultOK / ResultFailed are the agent's post-update self-report outcomes.
	ResultOK     = "ok"
	ResultFailed = "failed"

	healthFileName = "health.json"
	markerFileName = "update_marker.json"

	// maxFileBytes bounds the health/marker read (defense-in-depth against a
	// pathological/oversized file in the state dir). The real records are < 1 KiB.
	maxFileBytes = 64 << 10
)

var (
	// ErrAbsent is returned when the health/marker file does not exist yet.
	ErrAbsent = errors.New("health: file absent")
	// ErrMalformed is returned for invalid JSON or an unsupported schema_version.
	ErrMalformed = errors.New("health: malformed file")
	// ErrInvalid is returned when a record's fields are structurally invalid.
	ErrInvalid = errors.New("health: invalid record")
)

// Marker is the updater → agent handoff written before the swap: it carries the
// update_id the agent must echo in its post-update health record.
type Marker struct {
	SchemaVersion int    `json:"schema_version"`
	UpdateID      string `json:"update_id"`
}

// Record is the agent → updater post-update self-report, written atomically after a
// successful post-update heartbeat. The updater's health gate validates it.
type Record struct {
	SchemaVersion int    `json:"schema_version"`
	UpdateID      string `json:"update_id"`
	Result        string `json:"result"`     // ResultOK | ResultFailed
	WrittenAt     string `json:"written_at"` // RFC 3339, agent's local clock (same host as the updater)
}

// HealthPath / MarkerPath return the files' paths within the state directory.
func HealthPath(stateDir string) string { return filepath.Join(stateDir, healthFileName) }
func MarkerPath(stateDir string) string { return filepath.Join(stateDir, markerFileName) }

// WriteMarker atomically writes the update marker into stateDir (updater side).
func WriteMarker(stateDir string, m Marker) error {
	if m.UpdateID == "" {
		return fmt.Errorf("%w: empty update_id", ErrInvalid)
	}
	m.SchemaVersion = SchemaVersion
	return writeJSONAtomic(MarkerPath(stateDir), m)
}

// ReadMarker reads + validates the update marker (agent side). ErrAbsent if none.
func ReadMarker(stateDir string) (Marker, error) {
	var m Marker
	if err := readJSON(MarkerPath(stateDir), &m); err != nil {
		return Marker{}, err
	}
	if m.SchemaVersion != SchemaVersion {
		return Marker{}, fmt.Errorf("%w: marker schema_version %d", ErrMalformed, m.SchemaVersion)
	}
	if m.UpdateID == "" {
		return Marker{}, fmt.Errorf("%w: marker has no update_id", ErrInvalid)
	}
	return m, nil
}

// RemoveMarker deletes the marker (no error if absent).
func RemoveMarker(stateDir string) error { return removeIfExists(MarkerPath(stateDir)) }

// WriteHealth atomically writes the health record into stateDir (agent side).
func WriteHealth(stateDir string, r Record) error {
	if r.UpdateID == "" {
		return fmt.Errorf("%w: empty update_id", ErrInvalid)
	}
	if r.Result != ResultOK && r.Result != ResultFailed {
		return fmt.Errorf("%w: result %q", ErrInvalid, r.Result)
	}
	if !validRFC3339(r.WrittenAt) {
		return fmt.Errorf("%w: written_at %q is not RFC3339", ErrInvalid, r.WrittenAt)
	}
	r.SchemaVersion = SchemaVersion
	return writeJSONAtomic(HealthPath(stateDir), r)
}

// ReadHealth reads + structurally validates the health record (updater side).
// ErrAbsent if none. Semantic freshness (update_id match, window) is the gate's job.
func ReadHealth(stateDir string) (Record, error) {
	var r Record
	if err := readJSON(HealthPath(stateDir), &r); err != nil {
		return Record{}, err
	}
	if r.SchemaVersion != SchemaVersion {
		return Record{}, fmt.Errorf("%w: health schema_version %d", ErrMalformed, r.SchemaVersion)
	}
	if r.UpdateID == "" || (r.Result != ResultOK && r.Result != ResultFailed) || !validRFC3339(r.WrittenAt) {
		return Record{}, fmt.Errorf("%w: %+v", ErrInvalid, r)
	}
	return r, nil
}

// RemoveHealth deletes the health record (no error if absent).
func RemoveHealth(stateDir string) error { return removeIfExists(HealthPath(stateDir)) }

// ---- internal atomic-file helpers -------------------------------------------

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // never leak the temp file on a failed promote
		return err
	}
	return nil
}

// validRFC3339 reports whether s is a parseable RFC3339 timestamp.
func validRFC3339(s string) bool {
	if s == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrAbsent
		}
		return err
	}
	defer f.Close() //nolint:errcheck // read-only
	// Bounded read (defense-in-depth): reject anything larger than maxFileBytes.
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxFileBytes {
		return fmt.Errorf("%w: file exceeds %d bytes", ErrMalformed, maxFileBytes)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
