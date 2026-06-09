// Package storage defines the StorageBackend contract Beyz Backup uses to read
// and write backup data to a storage target.
//
// Sprint 1 FREEZES THIS INTERFACE ONLY; no real backend is implemented. The
// customer-owned and Beyz Cloud targets (SMB, SFTP, S3, MinIO, Beyz Cloud) land
// in Sprint 7. Freezing the contract now lets the future backup, restore, and
// retention engines be written against a stable abstraction so Sprint 7 needs no
// breaking change (risk STO-2).
//
// Design notes (decisions frozen now so Sprint 7 inherits them):
//   - Every method is context-aware (cancellation/deadlines for network targets).
//   - I/O is streaming (Put takes io.Reader, Get returns io.ReadCloser): large
//     objects are never required to fit in memory, consistent with pkg/hashing.
//   - Object keys are opaque strings; the tenant-scoped chunk-key scheme
//     ({tenant_id}/v1/{algo}/{hex}, STO-5) is composed ABOVE this package, so
//     storage stays tenant-agnostic and free of any cyclic dependency.
//   - Behavioural variation between targets (immutability/WORM, resumable
//     uploads, conditional writes, listing) is advertised via Capabilities so
//     callers can adapt instead of assuming — e.g. S3 object-lock vs SMB/SFTP
//     which cannot enforce WORM (risk STO-3).
//   - Optional parameters use option structs (PutOptions/ListOptions) so new
//     options are additive and never break the method signatures.
//
// This package imports only the standard library.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// Sentinel errors returned by StorageBackend implementations. Match with errors.Is.
var (
	// ErrNotImplemented is returned by placeholder/no-op backends and by any
	// method a backend does not yet implement.
	ErrNotImplemented = errors.New("storage: not implemented")
	// ErrNotFound is returned when an object key does not exist.
	ErrNotFound = errors.New("storage: object not found")
	// ErrAlreadyExists is returned by a conditional (IfNotExists) Put when the
	// key already exists.
	ErrAlreadyExists = errors.New("storage: object already exists")
	// ErrUnsupported is returned when an operation or option is not supported by
	// the backend (consult Capabilities first).
	ErrUnsupported = errors.New("storage: operation not supported by backend")
	// ErrImmutable is returned when modifying or deleting an object that is under
	// a retention lock (WORM).
	ErrImmutable = errors.New("storage: object is immutable")
)

// Capabilities advertises what a StorageBackend supports. Callers should consult
// it before relying on optional behaviour, because targets differ widely.
type Capabilities struct {
	// Name is a short backend identifier, e.g. "noop", "s3", "smb".
	Name string
	// Immutable reports support for write-once / object-lock (WORM) retention.
	Immutable bool
	// Resumable reports support for resumable / multipart uploads of large objects.
	Resumable bool
	// ConditionalWrite reports support for IfNotExists conditional puts.
	ConditionalWrite bool
	// List reports support for enumeration via List.
	List bool
	// ServerSideEncryption reports server-side at-rest encryption. Informational
	// only: Beyz Backup encrypts client-side regardless (ADR-001).
	ServerSideEncryption bool
	// MaxObjectSize is the largest single object the backend accepts, in bytes.
	// Zero means unknown or unbounded.
	MaxObjectSize int64
}

// ObjectInfo is metadata describing a stored object.
type ObjectInfo struct {
	// Key is the object's key.
	Key string
	// Size is the object size in bytes.
	Size int64
	// LastModified is the object's last-modified time.
	LastModified time.Time
	// ETag is a backend-specific entity tag, if any.
	ETag string
	// Digest is the algorithm-tagged integrity digest (pkg/hashing format, e.g.
	// "blake3:...") stored with the object, or empty if none.
	Digest string
	// Immutable reports whether the object is currently under a retention lock.
	Immutable bool
	// Metadata is arbitrary user metadata attached to the object.
	Metadata map[string]string
}

// PutOptions carries optional parameters for Put. Zero values select sensible
// defaults. An option gated by a capability is honoured only when that
// capability is advertised (otherwise it is ignored or rejected with
// ErrUnsupported, per the backend).
type PutOptions struct {
	// ContentLength is the exact size in bytes if known, or -1 when streaming an
	// unknown length.
	ContentLength int64
	// ContentType is an optional MIME type hint.
	ContentType string
	// Digest is an optional algorithm-tagged integrity digest (pkg/hashing
	// format) to store as metadata and, if the backend supports it, verify.
	Digest string
	// Immutable requests write-once / object-lock semantics. Honoured only when
	// Capabilities().Immutable is true.
	Immutable bool
	// IfNotExists makes the write conditional: the backend returns
	// ErrAlreadyExists if the key already exists. Honoured only when
	// Capabilities().ConditionalWrite is true.
	IfNotExists bool
	// Metadata is arbitrary user metadata to attach to the object.
	Metadata map[string]string
}

// ListOptions filters and paginates a List call.
type ListOptions struct {
	// Prefix restricts results to keys with this prefix.
	Prefix string
	// ContinuationToken resumes a previous truncated listing.
	ContinuationToken string
	// MaxKeys caps the number of results; zero selects the backend default.
	MaxKeys int
}

// ListResult is a (possibly truncated) page of a listing.
type ListResult struct {
	// Objects is the page of object metadata.
	Objects []ObjectInfo
	// NextContinuationToken, when IsTruncated is true, resumes the listing.
	NextContinuationToken string
	// IsTruncated reports whether more results remain beyond this page.
	IsTruncated bool
}

// StorageBackend is the contract every Beyz Backup storage target implements.
//
// Implementations MUST be safe for concurrent use by multiple goroutines, and
// all methods accept a context for cancellation and deadlines.
type StorageBackend interface {
	// Capabilities reports the optional features this backend supports.
	Capabilities() Capabilities

	// Put stores the object read from r under key, streaming the data, and
	// returns the stored object's metadata. With opts.IfNotExists it returns
	// ErrAlreadyExists if key already exists (when ConditionalWrite is supported).
	Put(ctx context.Context, key string, r io.Reader, opts PutOptions) (ObjectInfo, error)

	// Get opens the object at key for reading. The caller MUST Close the returned
	// reader. It returns ErrNotFound if key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)

	// Stat returns metadata for the object at key without reading its contents.
	// It returns ErrNotFound if key does not exist.
	Stat(ctx context.Context, key string) (ObjectInfo, error)

	// Delete removes the object at key. It returns ErrNotFound if key does not
	// exist and ErrImmutable if the object is under a retention lock.
	Delete(ctx context.Context, key string) error

	// List enumerates objects, optionally filtered by prefix and paginated.
	List(ctx context.Context, opts ListOptions) (ListResult, error)
}
