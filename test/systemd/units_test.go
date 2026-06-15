// Package systemd statically validates the hand-authored Linux unit files in
// build/linux/ (S1-T20). It is the always-on gate (runs under `go test ./...` on
// any OS, since it only reads files); `systemd-analyze verify` (task lint:systemd)
// is the optional Linux-only lint. These assertions encode the frozen T20
// decisions: Type=notify, RestartPreventExitStatus=10 11 (FI-T18-1), root user,
// systemd-managed directories, the updater oneshot+timer, and no secrets in any unit.
package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unitDir = "../../build/linux"

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(unitDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		t.Fatalf("%s is empty", name)
	}
	return string(b)
}

// directiveKeys returns the set of directive keys (the token before '=') on
// non-comment, non-section lines.
func directiveKeys(content string) map[string]bool {
	keys := map[string]bool{}
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "[") {
			continue
		}
		if k, _, ok := strings.Cut(ln, "="); ok {
			keys[strings.TrimSpace(k)] = true
		}
	}
	return keys
}

// hasSection reports whether the content has an actual [Section] header line (not a
// mention inside a comment).
func hasSection(content, section string) bool {
	for _, ln := range strings.Split(content, "\n") {
		if strings.TrimSpace(ln) == section {
			return true
		}
	}
	return false
}

func mustContain(t *testing.T, name, content string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(content, s) {
			t.Errorf("%s: missing %q", name, s)
		}
	}
}

// No unit may carry a secret value or inject the enrollment token inline.
func assertNoSecrets(t *testing.T, name, content string) {
	t.Helper()
	for _, bad := range []string{"bzt_", "ast_", "Environment=BEYZ_ENROLLMENT_TOKEN="} {
		if strings.Contains(content, bad) {
			t.Errorf("%s: must not contain %q (no secret/token in a unit file)", name, bad)
		}
	}
}

func TestAgentUnit(t *testing.T) {
	c := read(t, "beyz-backup-agent.service")
	mustContain(t, "agent.service", c,
		"Type=notify",
		"ExecStart=/usr/local/bin/beyz-backup-agent",
		"--foreground",
		"--config /etc/beyz-backup/config.yaml",
		"Restart=on-failure",
		"RestartPreventExitStatus=10 11", // FI-T18-1: 401 re-enroll / 426 upgrade never loop
		"ConfigurationDirectory=beyz-backup",
		"StateDirectory=beyz-backup",
		"StateDirectoryMode=0700",
		"LogsDirectory=beyz-backup",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"AF_UNIX", // sd_notify needs an AF_UNIX datagram socket
		"WantedBy=multi-user.target",
	)
	// Root user (Sprint-1 decision): no User= override and no DynamicUser.
	keys := directiveKeys(c)
	if keys["User"] {
		t.Error("agent.service: must not set User= (runs as root in Sprint 1)")
	}
	if keys["DynamicUser"] {
		t.Error("agent.service: must not set DynamicUser=")
	}
	assertNoSecrets(t, "agent.service", c)
}

func TestUpdaterUnitIsOneshotNoRestart(t *testing.T) {
	c := read(t, "beyz-backup-updater.service")
	mustContain(t, "updater.service", c,
		"Type=oneshot",
		"ExecStart=/usr/local/bin/beyz-backup-updater",
		"apply",
	)
	// A oneshot must never auto-restart.
	if directiveKeys(c)["Restart"] {
		t.Error("updater.service: oneshot must not set Restart=")
	}
	// The oneshot is started by the timer, not enabled directly.
	if hasSection(c, "[Install]") {
		t.Error("updater.service: oneshot should have no [Install] section (it is timer-triggered)")
	}
	assertNoSecrets(t, "updater.service", c)
}

func TestUpdaterTimerExistsButOptIn(t *testing.T) {
	c := read(t, "beyz-backup-updater.timer")
	mustContain(t, "updater.timer", c,
		"[Timer]",
		"OnCalendar=",
		"Persistent=true",
		"Unit=beyz-backup-updater.service",
		"WantedBy=timers.target",
	)
	assertNoSecrets(t, "updater.timer", c)
}
