//go:build !windows

package state

import (
	"fmt"
	"os"
)

// secureDir creates dir (if needed) and enforces 0700 (owner-only).
func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("state: creating dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("state: chmod dir: %w", err)
	}
	return nil
}

// secureFile enforces 0600 (owner read/write only).
func secureFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("state: chmod file: %w", err)
	}
	return nil
}

// syncDir fsyncs a directory so a preceding rename is durable (POSIX).
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
