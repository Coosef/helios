package trust_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
)

const embeddedTestKeyID = "helios-update-ed25519-test-1"

func TestEmbeddedLoadsFailClosed(t *testing.T) {
	ks, err := trust.Embedded()
	if err != nil {
		t.Fatalf("embedded trust set must load: %v", err)
	}
	if ks.Len() != 1 {
		t.Errorf("embedded Len = %d, want 1", ks.Len())
	}
	if _, err := ks.Key(embeddedTestKeyID); err != nil {
		t.Errorf("embedded key %q not resolvable: %v", embeddedTestKeyID, err)
	}
	if _, err := ks.TrustedKey(embeddedTestKeyID, nil); err != nil {
		t.Errorf("embedded key not trusted: %v", err)
	}
}

// The embedded key (keyset.json) and the published frozen artifact
// build/keys/update_pub_test.pem must describe the SAME public key.
func TestEmbeddedKeyMatchesPublishedPEM(t *testing.T) {
	ks, err := trust.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := ks.Key(embeddedTestKeyID)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, err := os.ReadFile(filepath.Join("..", "..", "..", "build", "keys", "update_pub_test.pem"))
	if err != nil {
		t.Fatalf("reading build/keys/update_pub_test.pem: %v", err)
	}
	fromPEM, err := trust.PublicKeyFromPEM(pemBytes)
	if err != nil {
		t.Fatalf("PublicKeyFromPEM: %v", err)
	}
	if !bytes.Equal(embedded, fromPEM) {
		t.Error("keyset.json key != build/keys/update_pub_test.pem (drift)")
	}
}

// The committed embedded key must be PUBLIC only (AC-35): the repo must not carry
// any private-key material near the trust anchor.
func TestNoPrivateKeyCommitted(t *testing.T) {
	for _, p := range []string{
		filepath.Join("..", "..", "..", "build", "keys", "update_pub_test.pem"),
		"keyset.json",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		if bytes.Contains(b, []byte("PRIVATE KEY")) {
			t.Errorf("%s contains private-key material", p)
		}
	}
}

func validRegistry(t *testing.T, revoked bool) []byte {
	t.Helper()
	pub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(7))
	b64 := base64.StdEncoding.EncodeToString(pub)
	rev := "false"
	if revoked {
		rev = "true"
	}
	return []byte(`{"schema_version":1,"keys":[{"key_id":"k1","algorithm":"ed25519","public_key":"` + b64 + `","revoked":` + rev + `}]}`)
}

func TestParseKeySetValid(t *testing.T) {
	ks, err := trust.ParseKeySet(validRegistry(t, false))
	if err != nil {
		t.Fatalf("valid registry rejected: %v", err)
	}
	if _, err := ks.Key("k1"); err != nil {
		t.Errorf("Key(k1): %v", err)
	}
}

func TestParseKeySetFailClosed(t *testing.T) {
	pub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(7))
	b64 := base64.StdEncoding.EncodeToString(pub)
	shortB64 := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	cases := map[string]string{
		"invalid json":         `{not json`,
		"wrong schema_version": `{"schema_version":2,"keys":[{"key_id":"k1","algorithm":"ed25519","public_key":"` + b64 + `"}]}`,
		"unsupported algo":     `{"schema_version":1,"keys":[{"key_id":"k1","algorithm":"rsa","public_key":"` + b64 + `"}]}`,
		"bad base64":           `{"schema_version":1,"keys":[{"key_id":"k1","algorithm":"ed25519","public_key":"!!!notb64"}]}`,
		"wrong key length":     `{"schema_version":1,"keys":[{"key_id":"k1","algorithm":"ed25519","public_key":"` + shortB64 + `"}]}`,
		"empty keys":           `{"schema_version":1,"keys":[]}`,
		"empty key_id":         `{"schema_version":1,"keys":[{"key_id":"","algorithm":"ed25519","public_key":"` + b64 + `"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := trust.ParseKeySet([]byte(body)); err == nil {
				t.Errorf("ParseKeySet accepted %s, want fail-closed", name)
			}
		})
	}
}

func TestPublicKeyFromPEM(t *testing.T) {
	ks, _ := trust.Embedded()
	embedded, _ := ks.Key(embeddedTestKeyID)
	pemBytes, _ := os.ReadFile(filepath.Join("..", "..", "..", "build", "keys", "update_pub_test.pem"))
	got, err := trust.PublicKeyFromPEM(pemBytes)
	if err != nil {
		t.Fatalf("valid PEM: %v", err)
	}
	if !bytes.Equal(got, embedded) || len(got) != ed25519.PublicKeySize {
		t.Error("PublicKeyFromPEM mismatch")
	}
	for name, bad := range map[string]string{
		"not pem":       "definitely not pem",
		"empty":         "",
		"truncated":     "-----BEGIN PUBLIC KEY-----\nzzzz\n-----END PUBLIC KEY-----\n",
		"trailing data": string(pemBytes) + "extra-junk",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := trust.PublicKeyFromPEM([]byte(bad)); !errors.Is(err, trust.ErrMalformedKey) {
				t.Errorf("PublicKeyFromPEM(%s): err = %v, want ErrMalformedKey", name, err)
			}
		})
	}
}

// PublicKeyFromPEM must explicitly reject a PRIVATE KEY PEM block (public-key only,
// AC-35) — not merely fail structurally later.
func TestPublicKeyFromPEMRejectsPrivateKey(t *testing.T) {
	_, priv := trusttest.GenerateKeyPair()
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, err := trust.PublicKeyFromPEM(privPEM); !errors.Is(err, trust.ErrMalformedKey) {
		t.Errorf("PRIVATE KEY PEM: err = %v, want ErrMalformedKey", err)
	}
	// Other non-public block types are also refused up front.
	for _, typ := range []string{"OPENSSH PRIVATE KEY", "RSA PRIVATE KEY", "CERTIFICATE"} {
		blk := pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: []byte("xx")})
		if _, err := trust.PublicKeyFromPEM(blk); !errors.Is(err, trust.ErrMalformedKey) {
			t.Errorf("%s block: err = %v, want ErrMalformedKey", typ, err)
		}
	}
}
