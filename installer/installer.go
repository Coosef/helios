// Package installer carries the Sprint-1 Windows installer sources (Inno Setup
// S1-T29) and embeds them so the installer contract is validated by `go test`
// on every platform. The Inno BUILD itself is Windows-only (ISCC.exe) and runs
// in the non-blocking CI job; these embeds back the static-content tests in
// installer_test.go, which assert the layout / ACL / token / service / uninstall
// invariants that the .iss must uphold.
//
// This package is intentionally NOT imported by cmd/agent or cmd/updater: the
// updater must never read the enrollment token file, and nothing ships the
// installer sources inside the agent/updater binaries.
package installer

import _ "embed"

// InnoScript is the verbatim Inno Setup source (pre-ISCC, macros unexpanded).
//
//go:embed beyz-backup.iss
var InnoScript string

// ApplyACLsScript is the post-install ACL hardening helper invoked by the .iss.
//
//go:embed scripts/apply-acls.ps1
var ApplyACLsScript string
