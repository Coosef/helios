// Package trust holds the compile-time embedded Ed25519 public-key SET that is the
// update trust anchor (S1-T22, ADR-002). The manifest carries a key_id selecting
// which embedded key signed it; the updater accepts a signature from any embedded,
// NON-revoked key, and rejects a key_id listed in the (signed) manifest's
// key_revocation_list. A key set + revocation enables overlapping-key rotation
// without re-issuing the fleet.
//
// SCOPE (T22): this package models the trust SET and resolves a key_id to a
// trusted public key, fail-closed. It does NOT verify any signature — the Ed25519
// verification over the canonical manifest is S1-T23. It is public-key only: no
// private key is, or is derivable from, this package (the production signing key
// lives in an HSM/KMS; the embedded TEST key's private half lives in the CI secret
// manager, never the repo — ADR-002 #7, AC-35).
package trust

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

var (
	// ErrEmptyKeySet is returned when a trust set has no keys.
	ErrEmptyKeySet = errors.New("trust: empty key set")
	// ErrNoActiveKeys is returned when every key in a set is revoked (a set that
	// could never accept any signature is a misconfiguration — fail closed).
	ErrNoActiveKeys = errors.New("trust: no active (non-revoked) keys")
	// ErrDuplicateKeyID is returned when two entries share a key_id.
	ErrDuplicateKeyID = errors.New("trust: duplicate key_id")
	// ErrMalformedKey is returned for a missing key_id or an invalid Ed25519 key.
	ErrMalformedKey = errors.New("trust: malformed key")
	// ErrUnknownKey is returned when a key_id is not in the trust set.
	ErrUnknownKey = errors.New("trust: unknown key_id")
	// ErrRevokedKey is returned when a key_id is embedded-revoked or appears in the
	// manifest's key_revocation_list.
	ErrRevokedKey = errors.New("trust: revoked key_id")
)

// KeyEntry is one trusted key for KeySet construction.
type KeyEntry struct {
	KeyID     string
	PublicKey ed25519.PublicKey
	// Revoked marks an embedded key as compile-time revoked (kept for diagnostics
	// and so a rotation can ship the new key while explicitly retiring the old one).
	Revoked bool
}

// KeySet is an immutable set of trusted Ed25519 public keys indexed by key_id. It
// is safe for concurrent use (read-only after construction).
type KeySet struct {
	keys    map[string]ed25519.PublicKey // all known key_ids -> public key
	revoked map[string]struct{}          // embedded-revoked key_ids
}

// NewKeySet builds and validates a trust set. It fails closed on an empty set, a
// set with no active keys, a duplicate key_id, a missing key_id, or a key that is
// not a valid 32-byte Ed25519 public key.
func NewKeySet(entries []KeyEntry) (*KeySet, error) {
	if len(entries) == 0 {
		return nil, ErrEmptyKeySet
	}
	ks := &KeySet{
		keys:    make(map[string]ed25519.PublicKey, len(entries)),
		revoked: make(map[string]struct{}),
	}
	active := 0
	for _, e := range entries {
		if e.KeyID == "" {
			return nil, fmt.Errorf("%w: empty key_id", ErrMalformedKey)
		}
		if len(e.PublicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: key_id %q is %d bytes, want %d", ErrMalformedKey, e.KeyID, len(e.PublicKey), ed25519.PublicKeySize)
		}
		if _, dup := ks.keys[e.KeyID]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKeyID, e.KeyID)
		}
		// Defensive copy so the caller cannot mutate the stored key material.
		pk := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(pk, e.PublicKey)
		ks.keys[e.KeyID] = pk
		if e.Revoked {
			ks.revoked[e.KeyID] = struct{}{}
		} else {
			active++
		}
	}
	if active == 0 {
		return nil, ErrNoActiveKeys
	}
	return ks, nil
}

// Key returns the public key for keyID if it is in the set and not embedded-revoked.
// It does NOT consider a manifest revocation list — use TrustedKey for the full
// trust decision. Fail-closed: unknown -> ErrUnknownKey, embedded-revoked ->
// ErrRevokedKey.
func (ks *KeySet) Key(keyID string) (ed25519.PublicKey, error) {
	pk, ok := ks.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, keyID)
	}
	if _, revoked := ks.revoked[keyID]; revoked {
		return nil, fmt.Errorf("%w: %q (embedded revocation)", ErrRevokedKey, keyID)
	}
	return clonePub(pk), nil
}

// TrustedKey returns the public key for keyID only if it is embedded, not
// embedded-revoked, AND not present in manifestRevocationList (the signed
// manifest's key_revocation_list, ADR-002 #3). This is the full trust predicate
// the verifier (S1-T23) uses to select the key before checking the signature.
func (ks *KeySet) TrustedKey(keyID string, manifestRevocationList []string) (ed25519.PublicKey, error) {
	pk, err := ks.Key(keyID)
	if err != nil {
		return nil, err
	}
	for _, revoked := range manifestRevocationList {
		if revoked == keyID {
			return nil, fmt.Errorf("%w: %q (manifest revocation)", ErrRevokedKey, keyID)
		}
	}
	return pk, nil
}

// KeyIDs returns the key_ids in the set (including revoked ones), for diagnostics.
func (ks *KeySet) KeyIDs() []string {
	ids := make([]string, 0, len(ks.keys))
	for id := range ks.keys {
		ids = append(ids, id)
	}
	return ids
}

// Len returns the number of keys (including revoked) in the set.
func (ks *KeySet) Len() int { return len(ks.keys) }

// PublicKeyFromPEM parses a single PKIX/PEM-encoded Ed25519 public key. It rejects
// non-PEM input, non-Ed25519 keys, and trailing data.
func PublicKeyFromPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, rest := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block", ErrMalformedKey)
	}
	// Explicit public-key-only guard (AC-35 / ADR-002 #7): refuse any non-"PUBLIC
	// KEY" block up front — a PRIVATE/OPENSSH/RSA PRIVATE KEY block is rejected here
	// rather than relying solely on x509.ParsePKIXPublicKey's structural mismatch.
	if block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("%w: PEM block type %q, want %q (refusing non-public-key material)", ErrMalformedKey, block.Type, "PUBLIC KEY")
	}
	if len(trimSpace(rest)) != 0 {
		return nil, fmt.Errorf("%w: trailing data after PEM block", ErrMalformedKey)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedKey, err)
	}
	ed, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: not an Ed25519 key (%T)", ErrMalformedKey, pub)
	}
	return ed, nil
}

func clonePub(pk ed25519.PublicKey) ed25519.PublicKey {
	out := make(ed25519.PublicKey, len(pk))
	copy(out, pk)
	return out
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && isSpace(b[i]) {
		i++
	}
	for j > i && isSpace(b[j-1]) {
		j--
	}
	return b[i:j]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
