package health_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/health"
)

func TestMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteMarker(dir, health.Marker{UpdateID: "upd_abc"}); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	m, err := health.ReadMarker(dir)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if m.UpdateID != "upd_abc" || m.SchemaVersion != health.SchemaVersion {
		t.Errorf("marker = %+v", m)
	}
	if err := health.RemoveMarker(dir); err != nil {
		t.Fatalf("RemoveMarker: %v", err)
	}
	if _, err := health.ReadMarker(dir); !errors.Is(err, health.ErrAbsent) {
		t.Errorf("after remove: err = %v, want ErrAbsent", err)
	}
}

func TestHealthRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := health.Record{UpdateID: "upd_1", Result: health.ResultOK, WrittenAt: time.Now().UTC().Format(time.RFC3339)}
	if err := health.WriteHealth(dir, rec); err != nil {
		t.Fatalf("WriteHealth: %v", err)
	}
	got, err := health.ReadHealth(dir)
	if err != nil {
		t.Fatalf("ReadHealth: %v", err)
	}
	if got.UpdateID != "upd_1" || got.Result != health.ResultOK || got.SchemaVersion != health.SchemaVersion {
		t.Errorf("record = %+v", got)
	}
	// atomic write left no .tmp
	if _, err := os.Stat(health.HealthPath(dir) + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp file should not remain")
	}
}

func TestWriteValidation(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteMarker(dir, health.Marker{}); !errors.Is(err, health.ErrInvalid) {
		t.Errorf("empty marker update_id: %v", err)
	}
	bad := []health.Record{
		{UpdateID: "", Result: health.ResultOK, WrittenAt: "x"},
		{UpdateID: "u", Result: "weird", WrittenAt: "x"},
		{UpdateID: "u", Result: health.ResultOK, WrittenAt: ""},
	}
	for _, r := range bad {
		if err := health.WriteHealth(dir, r); !errors.Is(err, health.ErrInvalid) {
			t.Errorf("WriteHealth(%+v) = %v, want ErrInvalid", r, err)
		}
	}
}

func TestReadAbsentAndMalformed(t *testing.T) {
	dir := t.TempDir()
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Errorf("absent: %v", err)
	}
	if err := os.WriteFile(health.HealthPath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrMalformed) {
		t.Errorf("malformed: %v", err)
	}
	// wrong schema_version
	_ = os.WriteFile(health.HealthPath(dir), []byte(`{"schema_version":2,"update_id":"u","result":"ok","written_at":"x"}`), 0o600)
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrMalformed) {
		t.Errorf("schema bump: %v", err)
	}
	// missing fields
	_ = os.WriteFile(health.HealthPath(dir), []byte(`{"schema_version":1,"update_id":"","result":"ok","written_at":"x"}`), 0o600)
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrInvalid) {
		t.Errorf("missing update_id: %v", err)
	}
}

func TestPathsUnderStateDir(t *testing.T) {
	dir := "/var/lib/beyz-backup/state"
	if health.HealthPath(dir) != filepath.Join(dir, "health.json") {
		t.Error("health path")
	}
	if health.MarkerPath(dir) != filepath.Join(dir, "update_marker.json") {
		t.Error("marker path")
	}
}

func TestRemoveHealthAndMarkerIdempotent(t *testing.T) {
	dir := t.TempDir()
	// removing absent files is a no-op (no error)
	if err := health.RemoveHealth(dir); err != nil {
		t.Errorf("RemoveHealth(absent): %v", err)
	}
	if err := health.RemoveMarker(dir); err != nil {
		t.Errorf("RemoveMarker(absent): %v", err)
	}
	_ = health.WriteHealth(dir, health.Record{UpdateID: "u", Result: health.ResultOK, WrittenAt: "2026-01-02T03:04:05Z"})
	if err := health.RemoveHealth(dir); err != nil {
		t.Fatalf("RemoveHealth: %v", err)
	}
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrAbsent) {
		t.Errorf("after RemoveHealth: %v", err)
	}
}

func TestReadMarkerMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(health.MarkerPath(dir), []byte(`{"schema_version":9,"update_id":"u"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := health.ReadMarker(dir); !errors.Is(err, health.ErrMalformed) {
		t.Errorf("marker schema bump: %v", err)
	}
	_ = os.WriteFile(health.MarkerPath(dir), []byte(`{"schema_version":1,"update_id":""}`), 0o600)
	if _, err := health.ReadMarker(dir); !errors.Is(err, health.ErrInvalid) {
		t.Errorf("marker empty update_id: %v", err)
	}
}

func TestWriteAtomicFailsOnReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("cannot chmod")
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck
	err := health.WriteHealth(dir, health.Record{UpdateID: "u", Result: health.ResultOK, WrittenAt: "2026-01-02T03:04:05Z"})
	if err == nil {
		t.Error("WriteHealth into a read-only dir should fail")
	}
	// no leftover temp file
	_ = os.Chmod(dir, 0o700)
	if _, serr := os.Stat(health.HealthPath(dir) + ".tmp"); !errors.Is(serr, os.ErrNotExist) {
		t.Error("temp file must not leak on failure")
	}
}

func TestWrittenAtMustBeRFC3339(t *testing.T) {
	dir := t.TempDir()
	if err := health.WriteHealth(dir, health.Record{UpdateID: "u", Result: health.ResultOK, WrittenAt: "banana"}); !errors.Is(err, health.ErrInvalid) {
		t.Errorf("malformed written_at at write: %v, want ErrInvalid", err)
	}
	// a hand-written file with a bad written_at must be rejected at read time too
	if err := os.WriteFile(health.HealthPath(dir), []byte(`{"schema_version":1,"update_id":"u","result":"ok","written_at":"2099-13-40T99:99:99Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrInvalid) {
		t.Errorf("malformed written_at at read: %v, want ErrInvalid", err)
	}
}

func TestReadRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, (64<<10)+10)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(health.HealthPath(dir), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := health.ReadHealth(dir); !errors.Is(err, health.ErrMalformed) {
		t.Errorf("oversized file: %v, want ErrMalformed", err)
	}
}
