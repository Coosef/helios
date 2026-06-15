//go:build !windows

package state

// White-box (package state) tests for the unexported, fail-closed helpers that
// the black-box state_test.go cannot reach directly: the atomic-write error
// paths, the POSIX permission/durability helpers, and the non-Windows default
// (unsupported) secret protector. Each asserts a fail-closed BEHAVIOR (never a
// silent partial write or plaintext fallback), not merely line coverage.
//
// Build-tagged !windows because secureDir/secureFile/syncDir and the unsupported
// default protector are the non-Windows implementations; the Windows DPAPI path
// is exercised by the windows-test CI job. The coverage gate runs on Linux.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// atomicWriteFile fails closed when the temp file cannot be created (its parent
// directory does not exist): it returns an error and writes nothing at the target.
func TestAtomicWriteFileFailsClosedOnUncreatableTemp(t *testing.T) {
	target := filepath.Join(t.TempDir(), "no-such-subdir", "out")
	if err := atomicWriteFile(target, []byte("x"), 0o600); err == nil {
		t.Fatal("atomicWriteFile must fail when the temp dir is missing")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target must not exist after a failed write, stat err = %v", err)
	}
}

// atomicWriteFile fails closed when the final rename cannot complete (the target
// path is occupied by a directory) and leaves no stray temp file behind.
func TestAtomicWriteFileFailsClosedWhenTargetIsDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(target, []byte("x"), 0o600); err == nil {
		t.Fatal("atomicWriteFile must fail when the target is a directory")
	}
	// The temp file (created via the ".tmp-*" pattern) must have been cleaned up.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "occupied" {
			t.Errorf("stray temp file left behind after failed rename: %s", e.Name())
		}
	}
}

// secureDir fails closed when a regular file already occupies the directory path.
func TestSecureDirFailsWhenPathIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureDir(f); err == nil {
		t.Error("secureDir over a regular file must fail")
	}
}

// secureFile fails when the target file does not exist (chmod has nothing to lock).
func TestSecureFileFailsWhenAbsent(t *testing.T) {
	if err := secureFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("secureFile on a missing path must fail")
	}
}

// syncDir fails when the directory cannot be opened for the durability fsync.
func TestSyncDirFailsWhenAbsent(t *testing.T) {
	if err := syncDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("syncDir on a missing dir must fail")
	}
}

// The non-Windows default protector fails closed on BOTH Protect and Unprotect —
// it never silently returns plaintext — and reports a stable, non-empty name.
func TestUnsupportedDefaultProtectorFailsClosedBothWays(t *testing.T) {
	p := defaultProtector()
	if p.Name() == "" {
		t.Error("default protector name must be non-empty")
	}
	if _, err := p.Protect([]byte("secret")); !errors.Is(err, ErrUnsupportedProtection) {
		t.Errorf("Protect = %v, want ErrUnsupportedProtection", err)
	}
	if _, err := p.Unprotect([]byte("blob")); !errors.Is(err, ErrUnsupportedProtection) {
		t.Errorf("Unprotect = %v, want ErrUnsupportedProtection", err)
	}
}

// Open fails closed when the state "dir" path is a regular file: the directory
// cannot be secured, so no store is returned.
func TestOpenFailsWhenDirIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	prot, err := NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Dir: f, Protector: prot}); err == nil {
		t.Error("Open over a regular-file path must fail")
	}
}

// GetDeviceGUID fails closed on a present-but-blank guid file rather than
// returning an empty device identity (ARCH-7 integrity).
func TestGetDeviceGUIDBlankFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	prot, err := NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if err := os.WriteFile(s.guidPath, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeviceGUID(); !errors.Is(err, ErrInvalidState) {
		t.Errorf("GetDeviceGUID(blank file) = %v, want ErrInvalidState", err)
	}
}
