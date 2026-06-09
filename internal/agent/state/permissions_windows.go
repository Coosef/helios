//go:build windows

package state

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// stateSDDL grants Full Access to SYSTEM (SY) and BUILTIN\Administrators (BA).
// The DACL is PROTECTED + auto-inherit-disabled (PAI) so inherited ACEs — most
// importantly Users — are dropped. This is the real secret boundary (§0.4).
const stateSDDL = "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// secureDir creates dir (if needed) and locks its ACL to SYSTEM + Administrators.
func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state: creating dir: %w", err)
	}
	return applyLockedACL(dir)
}

// secureFile locks a file's ACL to SYSTEM + Administrators.
func secureFile(path string) error {
	return applyLockedACL(path)
}

// syncDir is a no-op on Windows: NTFS commits a rename via its journal, and a
// directory handle cannot be fsynced the POSIX way.
func syncDir(string) error { return nil }

func applyLockedACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString(stateSDDL)
	if err != nil {
		return fmt.Errorf("state: building security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("state: reading DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("state: locking ACL on %q: %w", path, err)
	}
	return nil
}
