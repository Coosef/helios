//go:build windows

package paths

import "testing"

// Regression guard (runs on the windows-test CI job): the Windows layout must stay
// byte-for-byte identical to the pre-T20 inline defaults, under %ProgramData% for
// data and %ProgramFiles% for the agent binary. ProgramData/ProgramFiles are
// pinned so the assertion is deterministic regardless of the runner's real values.
func TestDefaultWindowsLayout(t *testing.T) {
	t.Setenv("ProgramData", `C:\PD`)
	t.Setenv("ProgramFiles", `C:\PF`)
	p := Default()
	for _, c := range []struct{ name, got, want string }{
		{"BaseDir", p.BaseDir, `C:\PD\BeyzBackup`},
		{"ConfigPath", p.ConfigPath, `C:\PD\BeyzBackup\config.yaml`},
		{"StateDir", p.StateDir, `C:\PD\BeyzBackup\state`},
		{"UpdateDir", p.UpdateDir, `C:\PD\BeyzBackup\update`},
		{"LockPath", p.LockPath, `C:\PD\BeyzBackup\state\agent.lock`},
		{"AgentBinaryPath", p.AgentBinaryPath, `C:\PF\BeyzBackup\beyz-backup-agent.exe`},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// The fallbacks must hold when the install-root env vars are unset.
func TestDefaultWindowsFallbacks(t *testing.T) {
	t.Setenv("ProgramData", "")
	t.Setenv("ProgramFiles", "")
	p := Default()
	if p.BaseDir != `C:\ProgramData\BeyzBackup` {
		t.Errorf("BaseDir fallback = %q", p.BaseDir)
	}
	if p.AgentBinaryPath != `C:\Program Files\BeyzBackup\beyz-backup-agent.exe` {
		t.Errorf("AgentBinaryPath fallback = %q", p.AgentBinaryPath)
	}
}
