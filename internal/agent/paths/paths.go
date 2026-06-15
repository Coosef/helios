// Package paths centralizes the OS-default install/runtime path layout for the
// agent and updater (S1-T20). Before T20 these defaults were duplicated inline in
// cmd/agent and cmd/updater; they now live here in one place behind a build-tagged
// per-OS resolver.
//
// Windows paths are byte-for-byte identical to the pre-T20 inline defaults
// (%ProgramData%\BeyzBackup for data, %ProgramFiles%\BeyzBackup for the agent
// binary). Non-Windows resolves the Linux FHS layout (/etc, /var/lib, /usr/local/bin)
// that the systemd unit (build/linux/) is authored against; the same resolver is
// used on darwin so the layout is testable on the dev host.
package paths

import "path/filepath"

// Binary names (frozen identifiers).
const (
	AgentBinaryName   = "beyz-backup-agent"
	UpdaterBinaryName = "beyz-backup-updater"
)

// lockFileName is the single-instance lockfile inside StateDir.
const lockFileName = "agent.lock"

// Paths is the resolved OS-default install/runtime layout. Every value is an
// absolute path. Callers may override individual paths via flags; these are only
// the defaults applied when a flag is empty.
type Paths struct {
	// BaseDir is the data root (Windows: %ProgramData%\BeyzBackup; Linux: /var/lib/beyz-backup).
	BaseDir string
	// ConfigPath is the config.yaml path.
	ConfigPath string
	// StateDir is the protected state store directory (bbolt DB, device.guid, token, lock).
	StateDir string
	// UpdateDir is the updater staging directory.
	UpdateDir string
	// LockPath is the agent single-instance lockfile.
	LockPath string
	// AgentBinaryPath is where the agent executable is installed (the updater's swap
	// target and the systemd unit's ExecStart).
	AgentBinaryPath string
}

// Default returns the OS-default path layout. It is pure (no I/O) beyond reading
// the platform's install-root environment variables on Windows.
func Default() Paths {
	base := baseDir()
	state := filepath.Join(base, "state")
	return Paths{
		BaseDir:         base,
		ConfigPath:      configPath(),
		StateDir:        state,
		UpdateDir:       filepath.Join(base, "update"),
		LockPath:        filepath.Join(state, lockFileName),
		AgentBinaryPath: agentBinaryPath(),
	}
}
