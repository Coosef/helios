package service

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrAlreadyRunning is returned by acquireLock when another instance holds the
// single-instance lock.
var ErrAlreadyRunning = errors.New("service: another instance is already running")

// fileLock is an OS-advisory exclusive lock on a lockfile.
type fileLock struct{ f *os.File }

// acquireLock takes a non-blocking exclusive lock on path (creating it + its
// parent dir). It returns ErrAlreadyRunning if the lock is already held.
func acquireLock(path string) (*fileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, ErrAlreadyRunning
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = unlockFile(l.f)
	return l.f.Close()
}
