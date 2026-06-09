package state_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
)

func openStore(t *testing.T) (*state.Store, *state.InsecureTestProtector, string) {
	t.Helper()
	dir := t.TempDir()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatalf("NewInsecureTestProtector: %v", err)
	}
	s, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, prot, dir
}

func TestOpenCloseIdempotent(t *testing.T) {
	s, _, _ := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be nil, got %v", err)
	}
	if _, err := s.Get(state.KeyDeviceID); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("Get after close = %v, want ErrInvalidState", err)
	}
	if err := s.Put(state.KeyDeviceID, []byte("x")); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("Put after close = %v, want ErrInvalidState", err)
	}
}

func TestOpenRequiresDir(t *testing.T) {
	if _, err := state.Open(state.Options{}); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("Open(no dir) = %v, want ErrInvalidState", err)
	}
}

func TestPutGetDelete(t *testing.T) {
	s, _, _ := openStore(t)

	if err := s.Put(state.KeyDeviceID, []byte("dev_123")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(state.KeyDeviceID)
	if err != nil || string(got) != "dev_123" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := s.Delete(state.KeyDeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(state.KeyDeviceID); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(state.KeyDeviceID); err != nil {
		t.Errorf("Delete absent = %v, want nil", err)
	}
	if _, err := s.Get(""); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("Get(empty) = %v, want ErrInvalidState", err)
	}
}

func TestMissingKey(t *testing.T) {
	s, _, _ := openStore(t)
	if _, err := s.Get("nope"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSecret("nope"); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("GetSecret(missing) = %v, want ErrNotFound", err)
	}
}

func TestSecretWrapUnwrapAndNoPlaintextInDB(t *testing.T) {
	s, _, dir := openStore(t)
	secret := []byte("PRIVATE-KEY-BYTES-3f9a-do-not-leak")

	if err := s.PutSecret(state.SecretPrivateKey, secret); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	got, err := s.GetSecret(state.SecretPrivateKey)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("GetSecret = %q, %v", got, err)
	}

	// The plaintext secret must never appear in the bbolt file.
	dbBytes, err := os.ReadFile(filepath.Join(dir, "agent-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dbBytes, secret) {
		t.Error("plaintext secret found in bbolt file")
	}
}

func TestTamperedSecretFailsClosed(t *testing.T) {
	_, prot, _ := openStore(t)

	// Protector level: flipping a byte must fail the GCM tag.
	wrapped, err := prot.Protect([]byte("topsecret"))
	if err != nil {
		t.Fatal(err)
	}
	wrapped[len(wrapped)-1] ^= 0xFF
	if _, err := prot.Unprotect(wrapped); err == nil {
		t.Error("Unprotect(tampered) must fail")
	}
}

func TestSecretWrongKeyAndProtectorMismatchFailClosed(t *testing.T) {
	dir := t.TempDir()
	prot1, _ := state.NewInsecureTestProtector()
	secret := []byte("key-material")

	s1, err := state.Open(state.Options{Dir: dir, Protector: prot1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.PutSecret(state.SecretLicenseBlob, secret); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	// Reopen with a different key (same name) -> unwrap fails closed.
	prot2, _ := state.NewInsecureTestProtector()
	s2, err := state.Open(state.Options{Dir: dir, Protector: prot2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.GetSecret(state.SecretLicenseBlob); err == nil {
		t.Error("GetSecret with wrong key must fail closed")
	}
	_ = s2.Close()

	// Reopen with a different protector NAME -> ErrInvalidState (mismatch).
	s3, err := state.Open(state.Options{Dir: dir, Protector: renamedProtector{inner: prot1, name: "other"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s3.Close() }()
	if _, err := s3.GetSecret(state.SecretLicenseBlob); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("protector mismatch = %v, want ErrInvalidState", err)
	}
}

func TestUnsupportedProtectorNeverStoresPlaintext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows default protector is DPAPI, not unsupported")
	}
	dir := t.TempDir()
	s, err := state.Open(state.Options{Dir: dir}) // nil protector -> platform default (unsupported)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	secret := []byte("must-not-be-written-plaintext")
	if err := s.PutSecret(state.SecretPrivateKey, secret); !errors.Is(err, state.ErrUnsupportedProtection) {
		t.Fatalf("PutSecret = %v, want ErrUnsupportedProtection", err)
	}
	if _, err := s.GetSecret(state.SecretPrivateKey); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("nothing should have been stored, got %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	s, _, _ := openStore(t)
	v, err := s.SchemaVersion()
	if err != nil || v != state.CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, %v; want %d", v, err, state.CurrentSchemaVersion)
	}
}

func TestSchemaVersionNewerRejected(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	s, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// Bump the on-disk schema version beyond what this build supports.
	db, err := bolt.Open(filepath.Join(dir, "agent-state.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	newer := make([]byte, 4)
	binary.BigEndian.PutUint32(newer, state.CurrentSchemaVersion+1)
	if err := db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("meta")).Put([]byte("schema_version"), newer)
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if _, err := state.Open(state.Options{Dir: dir, Protector: prot}); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("Open(newer schema) = %v, want ErrInvalidState", err)
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not unix file modes")
	}
	s, _, dir := openStore(t)
	if err := s.SetDeviceGUID("guid-perm"); err != nil {
		t.Fatal(err)
	}

	mustMode := func(path string, want fs.FileMode) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), info.Mode().Perm(), want)
		}
	}
	mustMode(dir, 0o700)
	mustMode(filepath.Join(dir, "agent-state.db"), 0o600)
	mustMode(filepath.Join(dir, "device.guid"), 0o600)
}

func TestDeviceGUIDPersistenceAndWriteOnce(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	open := func() *state.Store {
		s, err := state.Open(state.Options{Dir: dir, Protector: prot})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	s := open()
	if _, err := s.GetDeviceGUID(); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("GetDeviceGUID(fresh) = %v, want ErrNotFound", err)
	}
	if err := s.SetDeviceGUID("guid-abc-123"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceGUID("guid-abc-123"); err != nil {
		t.Errorf("idempotent SetDeviceGUID = %v, want nil", err)
	}
	if err := s.SetDeviceGUID("different"); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("changing GUID = %v, want ErrInvalidState", err)
	}
	_ = s.Close()

	// Persists across reopen.
	s = open()
	if g, err := s.GetDeviceGUID(); err != nil || g != "guid-abc-123" {
		t.Fatalf("GetDeviceGUID after reopen = %q, %v", g, err)
	}
	_ = s.Close()

	// Survives a rebuilt DB (the GUID is a separate sidecar file, ARCH-7).
	if err := os.Remove(filepath.Join(dir, "agent-state.db")); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer func() { _ = s.Close() }()
	if g, err := s.GetDeviceGUID(); err != nil || g != "guid-abc-123" {
		t.Errorf("GUID must survive DB rebuild, got %q, %v", g, err)
	}
}

func TestAuditAppenderAppendHeadRecordsAndPersist(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	id := audit.Identity{DeviceGUID: "dev-guid", DeviceID: "dev_1", TenantID: "tnt_1", Source: audit.SourceAgent}

	s, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	em, err := audit.New(s.AuditAppender(), nil, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, et := range []string{"enroll.attempt", "enroll.succeeded", "service.started"} {
		if _, err := em.Emit(audit.Event{EventType: et, Category: audit.CategoryAuth, Severity: audit.SeverityInfo, Outcome: audit.OutcomeSuccess, Actor: "system"}); err != nil {
			t.Fatal(err)
		}
	}

	app := s.AuditAppender()
	recs, err := app.Records()
	if err != nil || len(recs) != 3 {
		t.Fatalf("Records = %d, %v; want 3", len(recs), err)
	}
	seq, hash, ok, err := app.Head()
	if err != nil || !ok || seq != 3 || hash == "" {
		t.Fatalf("Head = %d,%q,%v,%v", seq, hash, ok, err)
	}
	if err := audit.Verify(recs, "dev-guid"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	_ = s.Close()

	// Reopen: chain persists and resumes from seq 3.
	s, err = state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	seq, _, ok, err = s.AuditAppender().Head()
	if err != nil || !ok || seq != 3 {
		t.Fatalf("resumed Head = %d,%v,%v; want 3", seq, ok, err)
	}
	em2, _ := audit.New(s.AuditAppender(), nil, id)
	r, err := em2.Emit(audit.Event{EventType: "service.stopped", Category: audit.CategoryLifecycle, Severity: audit.SeverityInfo, Outcome: audit.OutcomeSuccess, Actor: "system"})
	if err != nil || r.Seq != 4 {
		t.Fatalf("post-resume emit = seq %d, %v; want 4", r.Seq, err)
	}
}

func TestConcurrentAccessSafety(t *testing.T) {
	s, _, _ := openStore(t)
	app := s.AuditAppender()
	em, _ := audit.New(app, nil, audit.Identity{DeviceGUID: "g", Source: audit.SourceAgent})

	const workers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, workers*4)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", n)
			if err := s.Put(key, []byte(fmt.Sprintf("v%d", n))); err != nil {
				errCh <- err
			}
			if _, err := s.Get(key); err != nil {
				errCh <- err
			}
			if err := s.PutSecret(key, []byte("secret")); err != nil {
				errCh <- err
			}
			if _, err := s.GetSecret(key); err != nil {
				errCh <- err
			}
			if _, err := em.Emit(audit.Event{EventType: "service.started", Category: audit.CategoryLifecycle, Severity: audit.SeverityInfo, Outcome: audit.OutcomeSuccess, Actor: "system"}); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent op error: %v", err)
	}

	recs, err := app.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != workers {
		t.Errorf("audit records = %d, want %d", len(recs), workers)
	}
}

func TestClosedStoreRejectsAllOps(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	s, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	app := s.AuditAppender()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	checks := map[string]func() error{
		"GetSecret":      func() error { _, e := s.GetSecret(state.SecretPrivateKey); return e },
		"PutSecret":      func() error { return s.PutSecret(state.SecretPrivateKey, []byte("x")) },
		"Delete":         func() error { return s.Delete(state.KeyDeviceID) },
		"SchemaVersion":  func() error { _, e := s.SchemaVersion(); return e },
		"GetDeviceGUID":  func() error { _, e := s.GetDeviceGUID(); return e },
		"SetDeviceGUID":  func() error { return s.SetDeviceGUID("g") },
		"AppenderAppend": func() error { return app.Append(audit.Record{Seq: 1}) },
		"AppenderHead":   func() error { _, _, _, e := app.Head(); return e },
		"AppenderRecs":   func() error { _, e := app.Records(); return e },
	}
	for name, fn := range checks {
		if err := fn(); !errors.Is(err, state.ErrInvalidState) {
			t.Errorf("%s after close = %v, want ErrInvalidState", name, err)
		}
	}
}

func TestInsecureProtectorShortCiphertext(t *testing.T) {
	prot, _ := state.NewInsecureTestProtector()
	if _, err := prot.Unprotect([]byte{1, 2, 3}); err == nil {
		t.Error("Unprotect(short) must error")
	}
}

func TestSchemaVersionCorruptRejected(t *testing.T) {
	for name, bad := range map[string][]byte{
		"short bytes": {0, 1},
		"zero value":  {0, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prot, _ := state.NewInsecureTestProtector()
			s, err := state.Open(state.Options{Dir: dir, Protector: prot})
			if err != nil {
				t.Fatal(err)
			}
			_ = s.Close()

			db, err := bolt.Open(filepath.Join(dir, "agent-state.db"), 0o600, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket([]byte("meta")).Put([]byte("schema_version"), bad)
			}); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()

			if _, err := state.Open(state.Options{Dir: dir, Protector: prot}); !errors.Is(err, state.ErrInvalidState) {
				t.Errorf("Open(corrupt schema) = %v, want ErrInvalidState", err)
			}
		})
	}
}

func TestAuditAppenderRejectsDuplicateSeq(t *testing.T) {
	s, _, _ := openStore(t)
	app := s.AuditAppender()
	if err := app.Append(audit.Record{Seq: 1, ThisHash: "blake3:a"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Append(audit.Record{Seq: 1, ThisHash: "blake3:b"}); !errors.Is(err, state.ErrInvalidState) {
		t.Errorf("duplicate seq Append = %v, want ErrInvalidState", err)
	}
}

func TestLocationIDKey(t *testing.T) {
	s, _, _ := openStore(t)

	// ADR-006: location_id is a NON-secret identifier (no name persisted).
	if err := s.Put(state.KeyLocationID, []byte("loc_01HXYZSITE001")); err != nil {
		t.Fatalf("Put(location_id): %v", err)
	}
	got, err := s.Get(state.KeyLocationID)
	if err != nil {
		t.Fatalf("Get(location_id): %v", err)
	}
	if string(got) != "loc_01HXYZSITE001" {
		t.Errorf("location_id = %q", got)
	}
	// Being a non-secret key, the secret API must reject it (key-classification guard).
	if err := s.PutSecret(state.KeyLocationID, []byte("x")); !errors.Is(err, state.ErrInvalidState) {
		t.Error("PutSecret(location_id) must be rejected (non-secret key)")
	}
	if _, err := s.GetSecret(state.KeyLocationID); !errors.Is(err, state.ErrInvalidState) {
		t.Error("GetSecret(location_id) must be rejected (non-secret key)")
	}
}

func TestKeyClassificationGuards(t *testing.T) {
	s, _, _ := openStore(t)

	// Secret keys are rejected by the plaintext API.
	if err := s.Put(state.SecretPrivateKey, []byte("x")); !errors.Is(err, state.ErrInvalidState) {
		t.Error("Put(secret key) must be rejected")
	}
	if _, err := s.Get(state.SecretSessionToken); !errors.Is(err, state.ErrInvalidState) {
		t.Error("Get(secret key) must be rejected")
	}
	if err := s.Delete(state.SecretLicenseBlob); !errors.Is(err, state.ErrInvalidState) {
		t.Error("Delete(secret key) must be rejected")
	}
	// Non-secret keys are rejected by the secret API.
	if err := s.PutSecret(state.KeyDeviceID, []byte("x")); !errors.Is(err, state.ErrInvalidState) {
		t.Error("PutSecret(non-secret key) must be rejected")
	}
	if _, err := s.GetSecret(state.KeyCertificate); !errors.Is(err, state.ErrInvalidState) {
		t.Error("GetSecret(non-secret key) must be rejected")
	}
}

// renamedProtector wraps a Protector but reports a different Name, to exercise
// the stored-protector mismatch path.
type renamedProtector struct {
	inner state.Protector
	name  string
}

func (r renamedProtector) Protect(p []byte) ([]byte, error)   { return r.inner.Protect(p) }
func (r renamedProtector) Unprotect(c []byte) ([]byte, error) { return r.inner.Unprotect(c) }
func (r renamedProtector) Name() string                       { return r.name }
