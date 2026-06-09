// Package state is the agent's machine-protected state store.
//
// It is a single bbolt file (state/agent-state.db) holding the agent's identity,
// credentials, runtime cursors, and the audit-spool chain, plus a separate
// write-once device.guid sidecar file (kept outside bbolt so the stable device
// identity survives a rebuilt/corrupt DB — ARCH-7).
//
// Secret-at-rest model (SEC-6, ARCH-8, §0.4):
//   - The directory ACL (Windows: SYSTEM + Administrators only, Users removed;
//     Linux/macOS: 0700 dir / 0600 file) is the REAL access boundary and is set
//     at create time, not best-effort.
//   - Secret values (private key, session token, license blob) are additionally
//     wrapped by a platform Protector (Windows DPAPI machine-scope) BEFORE being
//     written to bbolt — defense-in-depth against offline disk theft. Non-secret
//     values (cert, IDs, region, cursors, version) are plaintext inside the
//     ACL-locked store.
//   - On non-Windows there is no production secret protector yet: the default
//     protector fails closed (ErrUnsupportedProtection) so plaintext secrets are
//     never silently written. Tests inject an InsecureTestProtector.
//
// All mutations go through bbolt transactions (ACID/atomic); the device.guid
// sidecar is written write-temp -> fsync -> rename. Secrets are never logged.
//
// Scope (S1-T10): the store only. No identity generation, enrollment, or
// heartbeat logic.
package state

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// CurrentSchemaVersion is the on-disk state-store schema version this build writes.
const CurrentSchemaVersion uint32 = 1

const (
	dbFileName            = "agent-state.db"
	guidFileName          = "device.guid"
	secretEnvelopeVersion = 1
)

// Bucket names (the frozen agent record set; the updater FSM is intentionally NOT
// a bucket — it is a separate file owned by the updater process, per §0.1/§0.6).
const (
	bucketMeta        = "meta"
	bucketIdentity    = "identity"
	bucketCredentials = "credentials"
	bucketRuntime     = "runtime"
	bucketAuditSpool  = "audit-spool"
)

var allBuckets = []string{bucketMeta, bucketIdentity, bucketCredentials, bucketRuntime, bucketAuditSpool}

const keySchemaVersion = "schema_version"

// Non-secret state keys (use Get/Put/Delete; stored plaintext in the ACL-locked store).
const (
	KeyDeviceID    = "device_id"
	KeyTenantID    = "tenant_id"
	KeyParentOrgID = "parent_org_id"
	KeyRegion      = "region"
	KeyCertificate = "certificate" // public X.509 cert: non-secret
)

// Secret keys (use GetSecret/PutSecret; wrapped before storage).
const (
	SecretPrivateKey       = "agent_private_key"
	SecretSessionToken     = "agent_session_token"
	SecretLicenseBlob      = "license_blob"
	SecretWrappedDeviceKey = "wrapped_device_key" // reserved (ADR-001)
)

// Typed errors. Match with errors.Is.
var (
	ErrNotFound              = errors.New("state: key not found")
	ErrUnsupportedProtection = errors.New("state: secret protection is not supported on this platform")
	ErrInvalidState          = errors.New("state: invalid state")
)

// Key-classification sets enforce the secret/non-secret split at the API
// boundary (defense-in-depth): secrets must go through Put/GetSecret (wrapped),
// non-secrets through Put/Get (plaintext), so a caller cannot accidentally write
// a raw secret into the plaintext bucket.
var (
	secretKeySet = map[string]struct{}{
		SecretPrivateKey: {}, SecretSessionToken: {}, SecretLicenseBlob: {}, SecretWrappedDeviceKey: {},
	}
	nonSecretKeySet = map[string]struct{}{
		KeyDeviceID: {}, KeyTenantID: {}, KeyParentOrgID: {}, KeyRegion: {}, KeyCertificate: {},
	}
)

// Protector wraps and unwraps secret values at rest.
type Protector interface {
	Protect(plaintext []byte) ([]byte, error)
	Unprotect(ciphertext []byte) ([]byte, error)
	// Name identifies the protector; it is stored alongside each secret so a
	// mismatch (e.g. DB created with DPAPI, opened with a test protector) is
	// detected instead of silently producing garbage.
	Name() string
}

// secretEnvelope is the on-disk wrapper around a protected secret. The plaintext
// is NEVER stored; only Data (the protector output) is.
type secretEnvelope struct {
	Protector string `json:"protector"`
	Version   int    `json:"v"`
	Data      []byte `json:"data"`
}

// Options configures Open.
type Options struct {
	// Dir is the state directory (created and ACL/permission-locked if needed).
	Dir string
	// Protector wraps secret values. When nil, the platform default is used
	// (Windows: DPAPI machine-scope; other: fail-closed unsupported).
	Protector Protector
}

// Store is the machine-protected agent state store. It is safe for concurrent use.
type Store struct {
	mu        sync.RWMutex
	closed    bool
	db        *bolt.DB
	protector Protector
	dir       string
	dbPath    string
	guidPath  string
	guidMu    sync.Mutex // serializes write-once device.guid access
}

// Open creates/opens the state store under opts.Dir, enforcing directory and file
// permissions and the schema version.
func Open(opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: empty state dir", ErrInvalidState)
	}
	if err := secureDir(opts.Dir); err != nil {
		return nil, fmt.Errorf("state: securing dir: %w", err)
	}
	prot := opts.Protector
	if prot == nil {
		prot = defaultProtector()
	}

	dbPath := filepath.Join(opts.Dir, dbFileName)
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("state: opening db %q: %w", dbPath, err)
	}
	if err := secureFile(dbPath); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("state: securing db file: %w", err)
	}

	s := &Store{
		db:        db,
		protector: prot,
		dir:       opts.Dir,
		dbPath:    dbPath,
		guidPath:  filepath.Join(opts.Dir, guidFileName),
	}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// init creates the buckets and validates/sets the schema version.
func (s *Store) init() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, b := range allBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(b)); err != nil {
				return fmt.Errorf("state: creating bucket %q: %w", b, err)
			}
		}
		meta := tx.Bucket([]byte(bucketMeta))
		raw := meta.Get([]byte(keySchemaVersion))
		if raw == nil {
			return meta.Put([]byte(keySchemaVersion), itob32(CurrentSchemaVersion))
		}
		// A present-but-malformed version must fail closed (never fail-open to 0).
		if len(raw) != 4 {
			return fmt.Errorf("%w: corrupt schema_version (%d bytes)", ErrInvalidState, len(raw))
		}
		got := binary.BigEndian.Uint32(raw)
		if got == 0 {
			return fmt.Errorf("%w: invalid schema_version 0", ErrInvalidState)
		}
		if got > CurrentSchemaVersion {
			return fmt.Errorf("%w: db schema version %d is newer than supported %d", ErrInvalidState, got, CurrentSchemaVersion)
		}
		// got <= current: supported (future migrations would run here).
		return nil
	})
}

// Close closes the store. It is idempotent and safe under concurrent use.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// SchemaVersion returns the on-disk schema version.
func (s *Store) SchemaVersion() (uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	var raw []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		raw = append([]byte(nil), tx.Bucket([]byte(bucketMeta)).Get([]byte(keySchemaVersion))...)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(raw) != 4 {
		return 0, fmt.Errorf("%w: corrupt schema_version", ErrInvalidState)
	}
	return binary.BigEndian.Uint32(raw), nil
}

// Get returns a non-secret value, or ErrNotFound.
func (s *Store) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty key", ErrInvalidState)
	}
	if _, isSecret := secretKeySet[key]; isSecret {
		return nil, fmt.Errorf("%w: %q is a secret key; use GetSecret", ErrInvalidState, key)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketIdentity)).Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		out = append([]byte(nil), v...) // copy: bbolt value is only valid in-tx
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Put stores a non-secret value atomically.
func (s *Store) Put(key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidState)
	}
	if _, isSecret := secretKeySet[key]; isSecret {
		return fmt.Errorf("%w: %q is a secret key; use PutSecret", ErrInvalidState, key)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketIdentity)).Put([]byte(key), value)
	})
}

// Delete removes a non-secret value (no error if absent).
func (s *Store) Delete(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidState)
	}
	if _, isSecret := secretKeySet[key]; isSecret {
		return fmt.Errorf("%w: %q is a secret key; use PutSecret", ErrInvalidState, key)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketIdentity)).Delete([]byte(key))
	})
}

// PutSecret wraps value with the protector and stores it atomically. If the
// protector is unsupported, it returns ErrUnsupportedProtection and writes
// nothing (no plaintext is ever stored).
func (s *Store) PutSecret(key string, value []byte) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidState)
	}
	if _, isNonSecret := nonSecretKeySet[key]; isNonSecret {
		return fmt.Errorf("%w: %q is a non-secret key; use Put", ErrInvalidState, key)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	wrapped, err := s.protector.Protect(value)
	if err != nil {
		return err // e.g. ErrUnsupportedProtection — nothing is stored
	}
	blob, err := json.Marshal(secretEnvelope{
		Protector: s.protector.Name(),
		Version:   secretEnvelopeVersion,
		Data:      wrapped,
	})
	if err != nil {
		return fmt.Errorf("state: encoding secret envelope: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketCredentials)).Put([]byte(key), blob)
	})
}

// GetSecret reads and unwraps a secret. A tampered or wrong-protector blob fails
// closed (returns an error, never partial/garbage plaintext).
func (s *Store) GetSecret(key string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: empty key", ErrInvalidState)
	}
	if _, isNonSecret := nonSecretKeySet[key]; isNonSecret {
		return nil, fmt.Errorf("%w: %q is a non-secret key; use Get", ErrInvalidState, key)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	var blob []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketCredentials)).Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		blob = append([]byte(nil), v...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var env secretEnvelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, fmt.Errorf("%w: corrupt secret envelope: %v", ErrInvalidState, err)
	}
	if env.Protector != s.protector.Name() {
		return nil, fmt.Errorf("%w: secret wrapped with protector %q, current is %q", ErrInvalidState, env.Protector, s.protector.Name())
	}
	plaintext, err := s.protector.Unprotect(env.Data)
	if err != nil {
		return nil, fmt.Errorf("state: unwrapping secret: %w", err)
	}
	return plaintext, nil
}

func itob32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func itob64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
