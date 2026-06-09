// Package identity manages the agent's local cryptographic identity: a stable
// device GUID, an ECDSA P-256 keypair, the PKCS#10 CSR, and the SPKI fingerprint.
//
// Identity model (ADR-003 + S1-T11 addendum), three independent values:
//   - device_guid  — agent-generated UUIDv4, write-once, survives reinstall (the
//     continuity/license-seat anchor; persisted via the state sidecar, T10).
//   - device_id    — server-issued opaque primary identity (NOT produced here).
//   - spki_sha256  — sha256(SubjectPublicKeyInfo DER) of the agent public key,
//     the credential fingerprint, formatted "sha256:<hex>".
//
// Everything is generated LOCALLY (no server dependency), keeping offline
// enrollment open. The private key is ECDSA P-256, stored PKCS#8 and
// Protector-wrapped in the state store; it never leaves the device and is never
// logged. Advisory hardware signals are collected privacy-preserving (hashed).
//
// Scope (S1-T11): identity material only. No enrollment, no certificate issuance,
// no renewal orchestration.
package identity

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/beyzbackup/beyz-backup/internal/agent/state"
)

// ErrInvalidKey indicates a missing/invalid or wrong-type identity key.
var ErrInvalidKey = errors.New("identity: invalid key")

// Material is the public identity bundle used to build an enrollment/registration
// request. The private key is NOT included — it stays wrapped in the state store.
type Material struct {
	DeviceGUID      string
	SPKIFingerprint string // "sha256:<hex>"
	CSRPEM          []byte
	HardwareSignals HardwareSignals
}

// Manager owns the agent's local identity, backed by the protected state store.
// It is safe for concurrent use; Ensure* calls are serialized.
type Manager struct {
	mu    sync.Mutex
	store *state.Store
}

// New returns a Manager backed by store.
func New(store *state.Store) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("identity: nil state store")
	}
	return &Manager{store: store}, nil
}

// EnsureDeviceGUID returns the persisted device GUID, generating and persisting a
// new UUIDv4 (write-once) if none exists.
func (m *Manager) EnsureDeviceGUID() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureDeviceGUID()
}

func (m *Manager) ensureDeviceGUID() (string, error) {
	guid, err := m.store.GetDeviceGUID()
	if err == nil {
		return guid, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return "", fmt.Errorf("identity: reading device guid: %w", err)
	}
	g, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("identity: generating device guid: %w", err)
	}
	guid = g.String()
	if err := m.store.SetDeviceGUID(guid); err != nil {
		// Possibly set concurrently elsewhere: re-read the authoritative value.
		if existing, rerr := m.store.GetDeviceGUID(); rerr == nil {
			return existing, nil
		}
		return "", fmt.Errorf("identity: persisting device guid: %w", err)
	}
	return guid, nil
}

// EnsureKeyPair returns the agent's ECDSA P-256 private key, generating and
// persisting it (PKCS#8, Protector-wrapped) if absent. The returned key is the
// only in-memory copy; it is never logged.
func (m *Manager) EnsureKeyPair() (*ecdsa.PrivateKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureKeyPair()
}

func (m *Manager) ensureKeyPair() (*ecdsa.PrivateKey, error) {
	der, err := m.store.GetSecret(state.SecretPrivateKey)
	if err == nil {
		return ParsePrivateKeyPKCS8(der)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return nil, fmt.Errorf("identity: reading private key: %w", err)
	}
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	pkcs8, err := MarshalPrivateKeyPKCS8(key)
	if err != nil {
		return nil, err
	}
	if err := m.store.PutSecret(state.SecretPrivateKey, pkcs8); err != nil {
		return nil, fmt.Errorf("identity: persisting private key: %w", err)
	}
	return key, nil
}

// Ensure loads-or-creates the device GUID and keypair (persisting both), builds a
// fresh CSR, and computes the SPKI fingerprint + hashed hardware signals. Only
// public enrollment material is returned; the private key stays in the store.
func (m *Manager) Ensure() (*Material, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	guid, err := m.ensureDeviceGUID()
	if err != nil {
		return nil, err
	}
	key, err := m.ensureKeyPair()
	if err != nil {
		return nil, err
	}
	fp, err := SPKIFingerprint(key.Public())
	if err != nil {
		return nil, err
	}
	csr, err := GenerateCSR(key, guid)
	if err != nil {
		return nil, err
	}
	return &Material{
		DeviceGUID:      guid,
		SPKIFingerprint: fp,
		CSRPEM:          csr,
		HardwareSignals: CollectHardwareSignals(),
	}, nil
}
