package trusttest_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
)

func TestDeterministicKeyPairReproducible(t *testing.T) {
	seed := trusttest.SeedFromByte(42)
	p1, k1 := trusttest.DeterministicKeyPair(seed)
	p2, k2 := trusttest.DeterministicKeyPair(seed)
	if !bytes.Equal(p1, p2) || !bytes.Equal(k1, k2) {
		t.Error("same seed must yield identical keypair")
	}
	if len(p1) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(p1), ed25519.PublicKeySize)
	}
	// The keypair is internally consistent (sign/verify round-trip).
	msg := []byte("x")
	if !ed25519.Verify(p1, msg, ed25519.Sign(k1, msg)) {
		t.Error("deterministic keypair sign/verify failed")
	}
}

func TestDeterministicKeyPairDistinctSeeds(t *testing.T) {
	p1, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	p2, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	if bytes.Equal(p1, p2) {
		t.Error("distinct seeds must yield distinct keys")
	}
}

func TestGenerateKeyPairRandom(t *testing.T) {
	p1, _ := trusttest.GenerateKeyPair()
	p2, _ := trusttest.GenerateKeyPair()
	if bytes.Equal(p1, p2) {
		t.Error("GenerateKeyPair must produce distinct keys")
	}
}
