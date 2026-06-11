//go:build !windows

package swap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// atomicReplace atomically renames src over dst (same volume) and fsyncs the parent
// directory for durability. A cross-volume rename (EXDEV) is reported as
// ErrCrossVolume — the staging dir must be on the live binary's volume.
func atomicReplace(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return fmt.Errorf("%w: %v", ErrCrossVolume, err)
		}
		return err
	}
	d, err := os.Open(filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
