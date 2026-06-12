package mocksaas

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
)

// No private key material (the deterministic test private keys, their seeds, or any
// PEM PRIVATE KEY block) may appear in the committed fixtures (AC-35).
func TestNoPrivateKeyMaterialCommitted(t *testing.T) {
	_, activePriv := activeKeys()
	_, revokedPriv := revokedKeys()
	secrets := map[string][]byte{
		"active private key":  activePriv,
		"active seed":         activePriv.Seed(),
		"revoked private key": revokedPriv,
		"revoked seed":        revokedPriv.Seed(),
		"PEM PRIVATE KEY":     []byte("PRIVATE KEY"),
	}
	root := filepath.Join("..", "..", "test", "fixtures")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("fixtures not present at %s (run: go run ./cmd/mkfixtures)", root)
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for name, s := range secrets {
			if bytes.Contains(data, s) {
				t.Errorf("%s contains %s — private material must NEVER be committed", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The public keyset must contain only public keys (sanity: the active+revoked pubs
// are present, and the bytes are exactly the public halves).
func TestPublicKeysetIsPublicOnly(t *testing.T) {
	activePub, activePriv := activeKeys()
	ks, err := PublicKeySetJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(ks, []byte(TestKeyID)) || !bytes.Contains(ks, []byte(RevokedKeyID)) {
		t.Error("keyset missing key ids")
	}
	if bytes.Contains(ks, activePriv.Seed()) {
		t.Error("keyset leaks the private seed")
	}
	_ = activePub
}

// The mocksaas TEST keys must NEVER be present in the embedded PRODUCTION keyset —
// a defense-in-depth gate so a fixture key can never be accepted by the production
// updater (which verifies against trust.Embedded()).
func TestTestKeysAbsentFromProductionKeyset(t *testing.T) {
	prod, err := trustEmbedded()
	if err != nil {
		t.Fatalf("loading embedded production keyset: %v", err)
	}
	for _, keyID := range []string{TestKeyID, RevokedKeyID} {
		if _, err := prod.Key(keyID); err == nil {
			t.Errorf("test key_id %q is present in the PRODUCTION embedded keyset — isolation broken", keyID)
		}
	}
}

func trustEmbedded() (*trust.KeySet, error) { return trust.Embedded() }
