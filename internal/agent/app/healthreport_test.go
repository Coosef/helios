package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

func TestReporterNoMarkerIsInert(t *testing.T) {
	dir := t.TempDir()
	r := newPostUpdateReporter(dir, nil)
	if r.report() != nil {
		t.Error("no marker: report() should be nil")
	}
	r.onBeatSuccess()
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Errorf("no marker: no health.json should be written, got %v", err)
	}
}

func TestReporterWritesHealthOnFirstSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteMarker(dir, health.Marker{UpdateID: "upd_99"}); err != nil {
		t.Fatal(err)
	}
	r := newPostUpdateReporter(dir, nil)

	got := r.report()
	if got == nil || *got != proto.UpdateResultOk {
		t.Fatalf("pending: report() = %v, want ok", got)
	}
	r.onBeatSuccess()

	rec, err := health.ReadHealth(dir)
	if err != nil {
		t.Fatalf("health.json not written: %v", err)
	}
	if rec.UpdateID != "upd_99" || rec.Result != health.ResultOK || rec.WrittenAt == "" {
		t.Errorf("record = %+v", rec)
	}
	// once written, the agent stops re-reporting
	if r.report() != nil {
		t.Error("after write: report() should be nil")
	}
}

func TestReporterWritesOnceAndStable(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteMarker(dir, health.Marker{UpdateID: "upd_x"}); err != nil {
		t.Fatal(err)
	}
	r := newPostUpdateReporter(dir, nil)
	r.onBeatSuccess()
	first, _ := os.ReadFile(health.HealthPath(dir))
	r.onBeatSuccess() // must be a no-op
	second, _ := os.ReadFile(health.HealthPath(dir))
	if string(first) != string(second) {
		t.Error("second onBeatSuccess must not rewrite health.json")
	}
}

func TestReporterRetriesAfterWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteMarker(dir, health.Marker{UpdateID: "upd_r"}); err != nil {
		t.Fatal(err)
	}
	r := newPostUpdateReporter(dir, nil) // reads marker while dir is writable

	// Make the directory read-only so the atomic write (create temp) fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("cannot chmod dir on this platform")
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck // restore for cleanup
	r.onBeatSuccess()
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Error("write should have failed; no health.json expected")
	}
	if r.report() == nil {
		t.Error("after a failed write the agent must keep reporting ok (retry)")
	}

	// Recover: dir writable again -> next success writes.
	_ = os.Chmod(dir, 0o700)
	r.onBeatSuccess()
	if _, err := os.Stat(filepath.Join(dir, "health.json")); err != nil {
		t.Errorf("retry write should succeed: %v", err)
	}
	if r.report() != nil {
		t.Error("after successful retry: report() should be nil")
	}
}

func TestReporterCorruptMarkerIsInert(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "update_marker.json"), []byte("{garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newPostUpdateReporter(dir, nil)
	if r.report() != nil {
		t.Error("corrupt marker: reporter must be inert (report nil)")
	}
	r.onBeatSuccess()
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Error("corrupt marker: no health.json should be written")
	}
}
