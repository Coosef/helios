package storage

import (
	"context"
	"io"
)

// NoopBackend is the Sprint 1 placeholder StorageBackend. It implements the full
// interface but performs no storage: every operation returns ErrNotImplemented
// with an empty result. It exists so code can be wired against StorageBackend
// before the real Sprint 7 backends exist, and is safe for concurrent use.
type NoopBackend struct{}

// Compile-time assertion that NoopBackend satisfies the interface.
var _ StorageBackend = (*NoopBackend)(nil)

// NewNoopBackend returns a new no-op storage backend.
func NewNoopBackend() *NoopBackend { return &NoopBackend{} }

// Capabilities reports no optional features.
func (*NoopBackend) Capabilities() Capabilities {
	return Capabilities{Name: "noop"}
}

// Put always returns ErrNotImplemented.
func (*NoopBackend) Put(_ context.Context, _ string, _ io.Reader, _ PutOptions) (ObjectInfo, error) {
	return ObjectInfo{}, ErrNotImplemented
}

// Get always returns ErrNotImplemented.
func (*NoopBackend) Get(_ context.Context, _ string) (io.ReadCloser, ObjectInfo, error) {
	return nil, ObjectInfo{}, ErrNotImplemented
}

// Stat always returns ErrNotImplemented.
func (*NoopBackend) Stat(_ context.Context, _ string) (ObjectInfo, error) {
	return ObjectInfo{}, ErrNotImplemented
}

// Delete always returns ErrNotImplemented.
func (*NoopBackend) Delete(_ context.Context, _ string) error {
	return ErrNotImplemented
}

// List always returns ErrNotImplemented.
func (*NoopBackend) List(_ context.Context, _ ListOptions) (ListResult, error) {
	return ListResult{}, ErrNotImplemented
}
