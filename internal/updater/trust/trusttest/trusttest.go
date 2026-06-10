// Package trusttest provides test-only helpers for building update trust sets and
// generating Ed25519 test keypairs. It is SEPARATE from the embedded trust anchor:
// these keypairs are generated at test runtime (their private halves never touch
// the repo), so unit tests can sign + verify fixtures without ever needing the
// embedded TEST key's private half (which lives in the CI secret manager).
package trusttest

import (
	"crypto/ed25519"
	"crypto/rand"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
)

// DeterministicKeyPair derives a reproducible Ed25519 keypair from a 32-byte seed,
// for tests that need stable fixtures across runs. The seed is supplied by the
// test (it is NOT the embedded key's seed); identical seeds yield identical keys.
func DeterministicKeyPair(seed [32]byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	priv := ed25519.NewKeyFromSeed(seed[:])
	return priv.Public().(ed25519.PublicKey), priv
}

// GenerateKeyPair returns a fresh random Ed25519 keypair for tests.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic("trusttest: generating key: " + err.Error())
	}
	return pub, priv
}

// SeedFromByte builds a 32-byte seed filled with b (a convenient distinct seed).
func SeedFromByte(b byte) [32]byte {
	var s [32]byte
	for i := range s {
		s[i] = b
	}
	return s
}

// KeySet builds a trust.KeySet from entries (a thin re-export so tests don't import
// both packages just to construct a set).
func KeySet(entries ...trust.KeyEntry) (*trust.KeySet, error) {
	return trust.NewKeySet(entries)
}

// SingleKeySet builds a one-key trust set from a public key.
func SingleKeySet(keyID string, pub ed25519.PublicKey) (*trust.KeySet, error) {
	return trust.NewKeySet([]trust.KeyEntry{{KeyID: keyID, PublicKey: pub}})
}
