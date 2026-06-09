package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetPopulatesRuntimeFields(t *testing.T) {
	info := Get("beyz-backup-agent")

	if info.Name != "beyz-backup-agent" {
		t.Fatalf("Name = %q, want %q", info.Name, "beyz-backup-agent")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != wantPlatform {
		t.Errorf("Platform = %q, want %q", info.Platform, wantPlatform)
	}
	// Defaults must be present unless overridden by ldflags; never empty.
	if info.Version == "" || info.Commit == "" || info.Date == "" {
		t.Errorf("build metadata must not be empty: %+v", info)
	}
}

func TestStringContainsKeyFields(t *testing.T) {
	info := Get("beyz-backup-updater")
	s := info.String()

	for _, want := range []string{"beyz-backup-updater", info.Version, info.GoVersion, info.Platform} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}
