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

	for _, want := range []string{"beyz-backup-updater", info.Version, info.Channel, info.GoVersion, info.Platform} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}

func TestChannelDefaultAndExposed(t *testing.T) {
	// Default channel is "dev" for local / PR builds (overridden via -ldflags).
	if Channel != "dev" {
		t.Errorf("default Channel = %q, want %q", Channel, "dev")
	}
	info := Get("beyz-backup-agent")
	if info.Channel != Channel {
		t.Errorf("Info.Channel = %q, want %q", info.Channel, Channel)
	}
	if info.Channel == "" {
		t.Error("Info.Channel must never be empty")
	}
	// the channel is surfaced in --version output.
	if !strings.Contains(info.String(), "channel "+info.Channel) {
		t.Errorf("String() = %q, missing channel", info.String())
	}
}
