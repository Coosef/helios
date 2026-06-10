package trust

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// KeySetSchemaVersion is the only embedded-keyset registry version understood.
const KeySetSchemaVersion = 1

// AlgorithmEd25519 is the only signing algorithm supported in Sprint 1.
const AlgorithmEd25519 = "ed25519"

// ErrUnsupportedKeySetVersion is returned for an unknown registry schema_version.
var ErrUnsupportedKeySetVersion = errors.New("trust: unsupported keyset schema_version")

// embeddedKeySetJSON is the compile-time trust anchor: the public-key SET baked
// into the (Authenticode-signed) binary. Sprint 1 ships a TEST public key; the
// production build embeds the real HSM-backed key(s). Public-key only — no private
// key is, or is derivable from, this file (ADR-002 #7, AC-35).
//
//go:embed keyset.json
var embeddedKeySetJSON []byte

// embeddedKeySet and embeddedErr are computed once at init; Embedded returns them.
var (
	embeddedKeySet *KeySet
	embeddedErr    error
)

func init() {
	embeddedKeySet, embeddedErr = ParseKeySet(embeddedKeySetJSON)
}

// Embedded returns the compile-time embedded trust set, or a fail-closed error if
// the embedded registry is missing, malformed, empty, or fully revoked. Callers
// MUST treat a non-nil error as "no trust anchor" and refuse to apply any update.
func Embedded() (*KeySet, error) {
	if embeddedErr != nil {
		return nil, embeddedErr
	}
	return embeddedKeySet, nil
}

// registry is the on-disk shape of the embedded keyset.
type registry struct {
	SchemaVersion int           `json:"schema_version"`
	Keys          []registryKey `json:"keys"`
}

type registryKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"` // base64 (std) raw 32-byte Ed25519 public key
	Revoked   bool   `json:"revoked"`
}

// ParseKeySet parses and validates a keyset registry (the embedded keyset.json
// format) into a trust set. It fails closed on invalid JSON, an unsupported
// schema_version, an unsupported algorithm, a non-base64 / wrong-length key, and
// everything NewKeySet rejects (empty, duplicate key_id, all-revoked).
//
// FORWARD NOTE (FI-T22-1): the only caller today is init() over the compile-time
// //go:embed'd, Authenticode-signed blob, so JSON laxity (encoding/json takes the
// last value for a duplicated key and ignores unknown fields) is not a runtime
// trust-bypass surface. If this parser is ever reused on an EXTERNALLY-supplied
// keyset, add a duplicate-JSON-key guard (last-wins is a key-confusion vector)
// before that reuse.
func ParseKeySet(data []byte) (*KeySet, error) {
	var r registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("trust: parsing keyset: %w", err)
	}
	if r.SchemaVersion != KeySetSchemaVersion {
		return nil, fmt.Errorf("%w: %d (want %d)", ErrUnsupportedKeySetVersion, r.SchemaVersion, KeySetSchemaVersion)
	}
	entries := make([]KeyEntry, 0, len(r.Keys))
	for i, k := range r.Keys {
		if k.Algorithm != AlgorithmEd25519 {
			return nil, fmt.Errorf("%w: keys[%d] %q has unsupported algorithm %q", ErrMalformedKey, i, k.KeyID, k.Algorithm)
		}
		raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("%w: keys[%d] %q: invalid base64: %v", ErrMalformedKey, i, k.KeyID, err)
		}
		entries = append(entries, KeyEntry{
			KeyID:     k.KeyID,
			PublicKey: ed25519.PublicKey(raw),
			Revoked:   k.Revoked,
		})
	}
	return NewKeySet(entries) // validates length, dup, empty, all-revoked
}
