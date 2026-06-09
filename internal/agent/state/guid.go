package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetDeviceGUID returns the persisted device GUID, or ErrNotFound if unset.
func (s *Store) GetDeviceGUID() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", fmt.Errorf("%w: store is closed", ErrInvalidState)
	}

	s.guidMu.Lock()
	defer s.guidMu.Unlock()

	b, err := os.ReadFile(s.guidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("state: reading device guid: %w", err)
	}
	guid := strings.TrimSpace(string(b))
	if guid == "" {
		return "", fmt.Errorf("%w: empty device guid file", ErrInvalidState)
	}
	return guid, nil
}

// SetDeviceGUID writes the device GUID once. It is idempotent if the same value
// is set again, and returns ErrInvalidState on an attempt to change an existing
// GUID. The GUID is a plaintext sidecar file (state/device.guid) kept outside
// bbolt so the stable device identity survives a rebuilt DB (ARCH-7). The write
// is atomic (write-temp -> fsync -> rename).
func (s *Store) SetDeviceGUID(guid string) error {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return fmt.Errorf("%w: empty device guid", ErrInvalidState)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("%w: store is closed", ErrInvalidState)
	}

	s.guidMu.Lock()
	defer s.guidMu.Unlock()

	existing, err := os.ReadFile(s.guidPath)
	if err == nil {
		if strings.TrimSpace(string(existing)) == guid {
			return nil // write-once, idempotent
		}
		return fmt.Errorf("%w: device guid is already set and immutable", ErrInvalidState)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("state: checking device guid: %w", err)
	}
	return atomicWriteFile(s.guidPath, []byte(guid+"\n"), 0o600)
}

// atomicWriteFile writes data to path via a temp file in the same directory,
// fsync, permission-lock, then rename (atomic on POSIX and Windows).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("state: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("state: setting temp file mode: %w", err)
	}
	if err := secureFile(tmpName); err != nil {
		return fmt.Errorf("state: securing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("state: renaming temp file: %w", err)
	}
	// fsync the parent directory so the rename (directory-entry change) is
	// durable across a crash — otherwise a power loss right after a successful
	// SetDeviceGUID could lose the write-once GUID entirely (POSIX).
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("state: syncing dir: %w", err)
	}
	return nil
}
