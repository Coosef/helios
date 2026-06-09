// Package buildinfo exposes version and build metadata for the Beyz Backup
// binaries (agent and updater).
//
// The Version, Commit, and Date variables are injected at link time via
// -ldflags; the defaults below are used only for local/dev builds. No secrets
// or environment-specific configuration are stored here (CLAUDE.md).
package buildinfo

import (
	"fmt"
	"runtime"
)

// Build-time-injected metadata. Override with, for example:
//
//	go build -ldflags "\
//	  -X github.com/beyzbackup/beyz-backup/internal/buildinfo.Version=1.4.0 \
//	  -X github.com/beyzbackup/beyz-backup/internal/buildinfo.Commit=abc1234 \
//	  -X github.com/beyzbackup/beyz-backup/internal/buildinfo.Date=2026-06-08T00:00:00Z"
var (
	// Version is the semantic version of the binary (e.g. "1.4.0").
	Version = "0.0.0-dev"
	// Commit is the source revision the binary was built from.
	Commit = "unknown"
	// Date is the UTC build timestamp in RFC 3339 form.
	Date = "unknown"
)

// Info is an immutable snapshot of build metadata for a named binary.
type Info struct {
	Name      string
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

// Get returns the build metadata for the given binary name.
func Get(name string) Info {
	return Info{
		Name:      name,
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String renders a single-line, human-readable build identifier.
func (i Info) String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s, %s)",
		i.Name, i.Version, i.Commit, i.Date, i.GoVersion, i.Platform)
}
