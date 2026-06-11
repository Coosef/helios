package swap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// ErrNotExecutable is returned when the (hash-verified) artifact does not have a
// valid executable header for the target OS. This is a CORRUPTION / operator-safety
// gate, NOT a trust mechanism — the signed dual-hash remains authoritative. It
// catches a signing-pipeline mistake (a wrong/corrupt artifact) BEFORE the
// destructive swap rather than after a 90s health-gate failure.
var ErrNotExecutable = errors.New("swap: artifact is not a valid executable for the target OS")

// validateExecutableFile checks the magic bytes of the file at path for targetOS.
// Unknown targets are skipped (the manifest enum is windows/linux). The check is
// intentionally minimal (magic only — no arch/subsystem constraints) so it cannot
// false-reject a legitimate signed binary.
func validateExecutableFile(targetOS, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open: %v", ErrNotExecutable, err)
	}
	defer func() { _ = f.Close() }()
	switch targetOS {
	case "windows":
		return validatePE(f)
	case "linux":
		return validateELF(f)
	default:
		return nil // no validator for this target — sanity layer only
	}
}

// validatePE checks the DOS "MZ" stub plus the "PE\0\0" signature at e_lfanew.
func validatePE(f *os.File) error {
	var mz [2]byte
	if _, err := f.ReadAt(mz[:], 0); err != nil || mz[0] != 'M' || mz[1] != 'Z' {
		return fmt.Errorf("%w: missing MZ header", ErrNotExecutable)
	}
	var lfanew [4]byte
	if _, err := f.ReadAt(lfanew[:], 0x3C); err != nil {
		return fmt.Errorf("%w: truncated DOS header", ErrNotExecutable)
	}
	off := int64(binary.LittleEndian.Uint32(lfanew[:]))
	var sig [4]byte
	if _, err := f.ReadAt(sig[:], off); err != nil || sig[0] != 'P' || sig[1] != 'E' || sig[2] != 0 || sig[3] != 0 {
		return fmt.Errorf("%w: missing PE signature", ErrNotExecutable)
	}
	return nil
}

// validateELF checks the 4-byte ELF magic (0x7F 'E' 'L' 'F').
func validateELF(f *os.File) error {
	var m [4]byte
	if _, err := f.ReadAt(m[:], 0); err != nil || m[0] != 0x7F || m[1] != 'E' || m[2] != 'L' || m[3] != 'F' {
		return fmt.Errorf("%w: missing ELF magic", ErrNotExecutable)
	}
	return nil
}
