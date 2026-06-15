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
	// Version is the semantic version of the binary (e.g. "1.4.0"). It is sourced
	// from the repo-root VERSION file at build time — the single version source
	// shared with the Windows version-info resource, the Inno AppVersion, and the
	// artifact names (S1-T30).
	Version = "0.0.0-dev"
	// Commit is the source revision the binary was built from.
	Commit = "unknown"
	// Date is the UTC build timestamp in RFC 3339 form.
	Date = "unknown"
	// Channel is the release channel the binary was built for: "dev" for local /
	// PR builds, "stable"/"beta"/"canary" for releases. Injected via -ldflags;
	// never a secret.
	Channel = "dev"
)

// Info is an immutable snapshot of build metadata for a named binary.
type Info struct {
	Name      string
	Version   string
	Commit    string
	Date      string
	Channel   string
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
		Channel:   Channel,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// String renders a single-line, human-readable build identifier.
func (i Info) String() string {
	return fmt.Sprintf("%s %s (channel %s, commit %s, built %s, %s, %s)",
		i.Name, i.Version, i.Channel, i.Commit, i.Date, i.GoVersion, i.Platform)
}
