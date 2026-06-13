package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// enrollTokenFile is the one-shot enrollment token file the installer drops into
// the protected state directory (Windows target: C:\ProgramData\BeyzBackup\state\
// enroll-token). It is a single-use BEARER credential, so it is treated as the
// most sensitive state artifact:
//
//   - the installer writes it atomically under a SYSTEM-only ACL (set at folder
//     create time; the agent runs as LocalSystem and can read+delete it);
//   - the value is plaintext for Sprint 1 (the ACL is the boundary per §0.4;
//     DPAPI wrapping is deferred), is NEVER written to config.yaml, and the value
//     is NEVER logged;
//   - it is read once and DELETED on a definitive enrollment outcome (success or a
//     rejected/consumed token), and is purged whenever the device is already
//     enrolled so a planted file can neither linger nor force a re-enroll.
const enrollTokenFile = "enroll-token"

// tokenFileResult classifies a one-shot token file read so the caller can drive
// the (delete | preserve) lifecycle without ever handling the raw value here.
type tokenFileResult int

const (
	// tokenFileAbsent: no file present. No token; nothing to delete.
	tokenFileAbsent tokenFileResult = iota
	// tokenFileValid: present and non-empty after trimming. Usable token.
	tokenFileValid
	// tokenFileEmpty: present but empty/whitespace-only. A poison-pill that can
	// never enroll, so it is deleted immediately to avoid an UNENROLLED loop.
	tokenFileEmpty
	// tokenFileUnreadable: present but the read failed (not a not-exist error).
	// Fail closed as no token and PRESERVE the file (the failure may be transient;
	// a later start may read it).
	tokenFileUnreadable
)

// enrollTokenPath returns the absolute one-shot token file path inside stateDir.
func enrollTokenPath(stateDir string) string {
	return filepath.Join(stateDir, enrollTokenFile)
}

// readEnrollTokenFile reads and classifies the one-shot token file. It performs
// NO deletion (the caller owns the lifecycle) and never places the token value in
// the returned error. The token is trimmed of surrounding whitespace (frozen
// format: raw bytes, trimmed, no JSON, no metadata). A missing file is the benign
// absent case; any other read error is unreadable (fail-closed, preserve).
func readEnrollTokenFile(stateDir string) (token string, result tokenFileResult, readErr error) {
	b, err := os.ReadFile(enrollTokenPath(stateDir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", tokenFileAbsent, nil
		}
		return "", tokenFileUnreadable, err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", tokenFileEmpty, nil
	}
	return tok, tokenFileValid, nil
}

// purgeEnrollTokenFile best-effort removes the one-shot token file. A missing file
// is success (idempotent); a non-nil error is returned only on a real removal
// failure so the caller can log it (path only, never the value).
func purgeEnrollTokenFile(stateDir string) error {
	if err := os.Remove(enrollTokenPath(stateDir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
