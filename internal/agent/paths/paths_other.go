//go:build !windows

package paths

// The non-Windows layout is the Linux FHS arrangement the systemd unit
// (build/linux/) is authored against. The same resolver is used on darwin so the
// layout compiles and is testable on the dev host (Linux is the only deployment
// target; macOS is not a supported runtime).
//
//	config : /etc/beyz-backup/config.yaml      (ConfigurationDirectory=beyz-backup)
//	state  : /var/lib/beyz-backup/state        (StateDirectory=beyz-backup, root-only 0700)
//	update : /var/lib/beyz-backup/update
//	binary : /usr/local/bin/beyz-backup-agent  (systemd ExecStart + updater swap target)

func baseDir() string { return "/var/lib/beyz-backup" }

func configPath() string { return "/etc/beyz-backup/config.yaml" }

func agentBinaryPath() string { return "/usr/local/bin/" + AgentBinaryName }
