package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/app"
	"github.com/beyzbackup/beyz-backup/internal/agent/service"
	"github.com/beyzbackup/beyz-backup/internal/agent/trustpins"
)

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--version"}, &out, &errb); code != exitOK {
		t.Errorf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(out.String(), binaryName) {
		t.Errorf("version output missing binary name: %q", out.String())
	}
}

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, exitOK},
		{app.ErrEnrollmentRequired, exitReEnroll},
		{fmt.Errorf("wrapped: %w", app.ErrEnrollmentRequired), exitReEnroll},
		{app.ErrUpgradeRequired, exitUpgrade},
		{service.ErrAlreadyRunning, exitAlreadyRunning},
		{app.ErrTransportInit, exitError},
		{app.ErrConfig, exitError},
		{errors.New("other"), exitError},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err); got != c.want {
			t.Errorf("exitCodeFor(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestTrustpinsEmptyByDefaultFailsClosed(t *testing.T) {
	if pins := trustpins.Bootstrap(); len(pins) != 0 {
		t.Errorf("default bootstrap pins must be empty (fail-closed), got %v", pins)
	}
}

func TestServeFailsClosedWithoutSecretLeak(t *testing.T) {
	t.Setenv("BEYZ_ENROLLMENT_TOKEN", "bzt_secrettoken_must_not_leak")
	var errb bytes.Buffer
	// Default per-OS paths are not writable/present in tests, so app.New fails early
	// (config/state/transport) before the blocking service run. Assert: non-zero exit
	// and the enrollment token never appears in the startup output.
	code := serve("", false, &errb)
	if code == exitOK {
		t.Error("serve with no usable config and empty pins must fail (non-zero)")
	}
	if strings.Contains(errb.String(), "secrettoken") {
		t.Errorf("startup output leaked the enrollment token: %s", errb.String())
	}
}

func TestUnknownControlVerb(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errb); code != exitError {
		t.Errorf("unknown verb exit = %d, want %d", code, exitError)
	}
}
