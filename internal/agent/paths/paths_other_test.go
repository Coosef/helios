//go:build !windows

package paths

import "testing"

// On non-Windows the layout is the fixed Linux FHS arrangement the systemd unit is
// authored against. These literals are also asserted by the systemd unit-file test
// (test/systemd) and the install docs — keep them in sync.
func TestDefaultLinuxLayout(t *testing.T) {
	p := Default()
	for _, c := range []struct{ name, got, want string }{
		{"BaseDir", p.BaseDir, "/var/lib/beyz-backup"},
		{"ConfigPath", p.ConfigPath, "/etc/beyz-backup/config.yaml"},
		{"StateDir", p.StateDir, "/var/lib/beyz-backup/state"},
		{"UpdateDir", p.UpdateDir, "/var/lib/beyz-backup/update"},
		{"LockPath", p.LockPath, "/var/lib/beyz-backup/state/agent.lock"},
		{"AgentBinaryPath", p.AgentBinaryPath, "/usr/local/bin/beyz-backup-agent"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
