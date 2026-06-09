package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/storage"
)

func TestNoopBackendSatisfiesInterface(t *testing.T) {
	var b storage.StorageBackend = storage.NewNoopBackend()
	if b == nil {
		t.Fatal("NewNoopBackend returned nil")
	}
}

func TestNoopBackendReturnsNotImplemented(t *testing.T) {
	ctx := context.Background()
	var b storage.StorageBackend = storage.NewNoopBackend()

	if _, err := b.Put(ctx, "k", strings.NewReader("x"), storage.PutOptions{}); !errors.Is(err, storage.ErrNotImplemented) {
		t.Errorf("Put error = %v, want ErrNotImplemented", err)
	}
	rc, _, err := b.Get(ctx, "k")
	if !errors.Is(err, storage.ErrNotImplemented) || rc != nil {
		t.Errorf("Get = (%v, _, %v), want (nil, _, ErrNotImplemented)", rc, err)
	}
	if _, err := b.Stat(ctx, "k"); !errors.Is(err, storage.ErrNotImplemented) {
		t.Errorf("Stat error = %v, want ErrNotImplemented", err)
	}
	if err := b.Delete(ctx, "k"); !errors.Is(err, storage.ErrNotImplemented) {
		t.Errorf("Delete error = %v, want ErrNotImplemented", err)
	}
	res, err := b.List(ctx, storage.ListOptions{Prefix: "p/"})
	if !errors.Is(err, storage.ErrNotImplemented) {
		t.Errorf("List error = %v, want ErrNotImplemented", err)
	}
	if len(res.Objects) != 0 || res.IsTruncated || res.NextContinuationToken != "" {
		t.Errorf("List result must be empty, got %+v", res)
	}
}

func TestNoopCapabilities(t *testing.T) {
	c := storage.NewNoopBackend().Capabilities()
	if c.Name != "noop" {
		t.Errorf("Name = %q, want noop", c.Name)
	}
	if c.Immutable || c.Resumable || c.ConditionalWrite || c.List || c.ServerSideEncryption {
		t.Errorf("noop must advertise no capabilities, got %+v", c)
	}
	if c.MaxObjectSize != 0 {
		t.Errorf("MaxObjectSize = %d, want 0", c.MaxObjectSize)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{
		storage.ErrNotImplemented,
		storage.ErrNotFound,
		storage.ErrAlreadyExists,
		storage.ErrUnsupported,
		storage.ErrImmutable,
	}
	for i := range all {
		for j := range all {
			if i != j && errors.Is(all[i], all[j]) {
				t.Errorf("sentinel errors %d (%v) and %d (%v) are not distinct", i, all[i], j, all[j])
			}
		}
	}
}

// Compile-time + runtime proof that the frozen contract types exist with the
// documented fields (the Sprint 7 placeholder structs).
func TestContractTypesAreUsable(t *testing.T) {
	po := storage.PutOptions{
		ContentLength: -1,
		ContentType:   "application/octet-stream",
		Digest:        "blake3:deadbeef",
		Immutable:     true,
		IfNotExists:   true,
		Metadata:      map[string]string{"k": "v"},
	}
	lo := storage.ListOptions{Prefix: "tnt_x/v1/blake3/", ContinuationToken: "tok", MaxKeys: 100}
	lr := storage.ListResult{
		Objects:               []storage.ObjectInfo{{Key: "k", Size: 1, Digest: "sha256:abc"}},
		NextContinuationToken: "next",
		IsTruncated:           true,
	}
	caps := storage.Capabilities{
		Name: "s3", Immutable: true, Resumable: true, ConditionalWrite: true,
		List: true, ServerSideEncryption: true, MaxObjectSize: 5 << 40,
	}

	if !po.Immutable || lo.MaxKeys != 100 || !lr.IsTruncated || caps.Name != "s3" {
		t.Fatal("contract struct fields did not round-trip as set")
	}
}
