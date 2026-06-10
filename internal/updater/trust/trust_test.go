package trust_test

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
)

func TestValidKeyLookup(t *testing.T) {
	pub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	ks, err := trusttest.SingleKeySet("k1", pub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ks.Key("k1")
	if err != nil {
		t.Fatalf("Key(k1): %v", err)
	}
	if !bytes.Equal(got, pub) {
		t.Error("resolved key does not match the embedded public key")
	}
	if ks.Len() != 1 {
		t.Errorf("Len = %d, want 1", ks.Len())
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	pub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	ks, _ := trusttest.SingleKeySet("k1", pub)
	if _, err := ks.Key("nope"); !errors.Is(err, trust.ErrUnknownKey) {
		t.Errorf("Key(unknown): err = %v, want ErrUnknownKey", err)
	}
	if _, err := ks.TrustedKey("nope", nil); !errors.Is(err, trust.ErrUnknownKey) {
		t.Errorf("TrustedKey(unknown): err = %v, want ErrUnknownKey", err)
	}
}

func TestEmbeddedRevokedKeyRejected(t *testing.T) {
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	p2, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	ks, err := trusttest.KeySet(
		trust.KeyEntry{KeyID: "old", PublicKey: p1, Revoked: true},
		trust.KeyEntry{KeyID: "new", PublicKey: p2},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ks.Key("old"); !errors.Is(err, trust.ErrRevokedKey) {
		t.Errorf("Key(embedded-revoked): err = %v, want ErrRevokedKey", err)
	}
	if _, err := ks.Key("new"); err != nil {
		t.Errorf("Key(active): %v", err)
	}
}

func TestManifestRevocationRejected(t *testing.T) {
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	ks, _ := trusttest.SingleKeySet("k1", p1)
	if _, err := ks.TrustedKey("k1", []string{"other", "k1"}); !errors.Is(err, trust.ErrRevokedKey) {
		t.Errorf("TrustedKey(manifest-revoked): err = %v, want ErrRevokedKey", err)
	}
	// Not in the list -> trusted.
	if _, err := ks.TrustedKey("k1", []string{"other"}); err != nil {
		t.Errorf("TrustedKey(not-revoked): %v", err)
	}
}

func TestMultiKeyRotationScenario(t *testing.T) {
	// Two overlapping keys; a signed manifest later revokes the old one. The new key
	// stays trusted, the old becomes untrusted — rotation without re-issuing the fleet.
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	p2, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	ks, err := trusttest.KeySet(
		trust.KeyEntry{KeyID: "key-2025", PublicKey: p1},
		trust.KeyEntry{KeyID: "key-2026", PublicKey: p2},
	)
	if err != nil {
		t.Fatal(err)
	}
	revoke := []string{"key-2025"}
	if _, err := ks.TrustedKey("key-2025", revoke); !errors.Is(err, trust.ErrRevokedKey) {
		t.Errorf("old key after revocation: err = %v, want ErrRevokedKey", err)
	}
	got, err := ks.TrustedKey("key-2026", revoke)
	if err != nil {
		t.Fatalf("new key must stay trusted: %v", err)
	}
	if !bytes.Equal(got, p2) {
		t.Error("rotated-to key material mismatch")
	}
}

func TestMalformedKeyRejected(t *testing.T) {
	t.Run("wrong length", func(t *testing.T) {
		if _, err := trusttest.KeySet(trust.KeyEntry{KeyID: "k", PublicKey: ed25519.PublicKey{1, 2, 3}}); !errors.Is(err, trust.ErrMalformedKey) {
			t.Errorf("err = %v, want ErrMalformedKey", err)
		}
	})
	t.Run("nil key", func(t *testing.T) {
		if _, err := trusttest.KeySet(trust.KeyEntry{KeyID: "k", PublicKey: nil}); !errors.Is(err, trust.ErrMalformedKey) {
			t.Errorf("err = %v, want ErrMalformedKey", err)
		}
	})
	t.Run("empty key_id", func(t *testing.T) {
		pub, _ := trusttest.GenerateKeyPair()
		if _, err := trusttest.KeySet(trust.KeyEntry{KeyID: "", PublicKey: pub}); !errors.Is(err, trust.ErrMalformedKey) {
			t.Errorf("err = %v, want ErrMalformedKey", err)
		}
	})
}

func TestEmptyKeySetRejected(t *testing.T) {
	if _, err := trust.NewKeySet(nil); !errors.Is(err, trust.ErrEmptyKeySet) {
		t.Errorf("NewKeySet(nil): err = %v, want ErrEmptyKeySet", err)
	}
	if _, err := trust.NewKeySet([]trust.KeyEntry{}); !errors.Is(err, trust.ErrEmptyKeySet) {
		t.Errorf("NewKeySet(empty): err = %v, want ErrEmptyKeySet", err)
	}
}

func TestAllRevokedRejected(t *testing.T) {
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	if _, err := trusttest.KeySet(trust.KeyEntry{KeyID: "k", PublicKey: p1, Revoked: true}); !errors.Is(err, trust.ErrNoActiveKeys) {
		t.Errorf("all-revoked set: err = %v, want ErrNoActiveKeys", err)
	}
}

func TestDuplicateKeyIDRejected(t *testing.T) {
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	p2, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	if _, err := trusttest.KeySet(
		trust.KeyEntry{KeyID: "dup", PublicKey: p1},
		trust.KeyEntry{KeyID: "dup", PublicKey: p2},
	); !errors.Is(err, trust.ErrDuplicateKeyID) {
		t.Errorf("duplicate key_id: err = %v, want ErrDuplicateKeyID", err)
	}
}

// The KeySet must not be mutable through the caller's input slice or the returned
// key (defensive copies), so trust material cannot be tampered post-construction.
func TestKeySetIsImmutable(t *testing.T) {
	pub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	original := bytes.Clone(pub) // snapshot before any mutation
	entries := []trust.KeyEntry{{KeyID: "k1", PublicKey: pub}}
	ks, _ := trust.NewKeySet(entries)

	pub[0] ^= 0xff // mutate the caller's slice (== entries[0].PublicKey backing array)
	got, _ := ks.Key("k1")
	if !bytes.Equal(got, original) {
		t.Error("mutating the input slice changed stored key material")
	}

	got[0] ^= 0xff // mutate the returned key
	again, _ := ks.Key("k1")
	if !bytes.Equal(again, original) {
		t.Error("mutating a returned key changed stored key material")
	}
}

// Sanity: a key resolved from the set verifies a signature made by its private
// half (stdlib ed25519.Verify in the TEST only — signature verification itself is
// S1-T23; this just proves the trust set yields usable key material).
func TestResolvedKeyVerifiesSignature(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(9))
	ks, _ := trusttest.SingleKeySet("k1", pub)
	key, err := ks.TrustedKey("k1", nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("canonical-manifest-bytes")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(key, msg, sig) {
		t.Error("resolved key failed to verify a signature from its private half")
	}
	if ed25519.Verify(key, []byte("tampered"), sig) {
		t.Error("resolved key verified a signature over the wrong message")
	}
}
