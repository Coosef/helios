//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// baseDir is %ProgramData%\BeyzBackup (fallback C:\ProgramData). Byte-for-byte
// identical to the pre-T20 cmd/agent + cmd/updater inline default.
func baseDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "BeyzBackup")
}

// configPath is %ProgramData%\BeyzBackup\config.yaml.
func configPath() string { return filepath.Join(baseDir(), "config.yaml") }

// agentBinaryPath is %ProgramFiles%\BeyzBackup\beyz-backup-agent.exe (fallback
// C:\Program Files). Byte-for-byte identical to the pre-T20 updater default.
func agentBinaryPath() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "BeyzBackup", AgentBinaryName+".exe")
}
