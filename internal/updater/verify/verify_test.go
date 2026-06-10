package verify_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zeebo/blake3"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func validManifestMap() map[string]any {
	return map[string]any{
		"schema_version":        1,
		"target_version":        "1.2.0",
		"min_supported_version": "1.0.0",
		"artifacts": []any{map[string]any{
			"platform": "linux", "arch": "amd64", "url": "https://dl.example.com/agent",
			"size_bytes": 4096, "sha256": hex64, "blake3": hex64,
		}},
		"key_id":              "test-key-1",
		"key_revocation_list": []any{},
		"signature":           "placeholder",
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// signed returns m signed with priv (m["key_id"] must already be set).
func signed(t *testing.T, m map[string]any, priv ed25519.PrivateKey) []byte {
	t.Helper()
	out, err := trusttest.SignManifest(mustMarshal(t, m), priv)
	if err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	return out
}

func keySetFor(t *testing.T, keyID string, pub ed25519.PublicKey) *trust.KeySet {
	t.Helper()
	ks, err := trusttest.SingleKeySet(keyID, pub)
	if err != nil {
		t.Fatalf("SingleKeySet: %v", err)
	}
	return ks
}

// ---- manifest signature verification -----------------------------------------

func TestManifestValidSignaturePasses(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	m := validManifestMap()
	got, err := verify.Manifest(signed(t, m, priv), keySetFor(t, "test-key-1", pub))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if got.TargetVersion != "1.2.0" || got.KeyID != "test-key-1" {
		t.Errorf("verified manifest fields wrong: %+v", got)
	}
}

func TestManifestModifiedFieldFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	raw := signed(t, validManifestMap(), priv)
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	o["target_version"], _ = json.Marshal("9.9.9") // tamper a signed field, do not re-sign
	tampered := mustMarshal(t, o)
	if _, err := verify.Manifest(tampered, keySetFor(t, "test-key-1", pub)); !errors.Is(err, verify.ErrSignatureInvalid) {
		t.Errorf("modified field: err = %v, want ErrSignatureInvalid", err)
	}
}

// A manifest signed WITH an unknown field verifies (forward-compat: unknown fields
// are part of the canonical signing input).
func TestManifestSignedUnknownFieldVerifies(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	m := validManifestMap()
	m["future_field"] = map[string]any{"rollout": 42}
	if _, err := verify.Manifest(signed(t, m, priv), keySetFor(t, "test-key-1", pub)); err != nil {
		t.Errorf("manifest with a signed unknown field rejected: %v", err)
	}
}

// Adding an unknown field AFTER signing breaks verification (unknown fields are
// included in the signing input and therefore protected).
func TestManifestUnknownFieldAddedAfterSigningFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	raw := signed(t, validManifestMap(), priv)
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	o["evil_injected"], _ = json.Marshal("payload")
	if _, err := verify.Manifest(mustMarshal(t, o), keySetFor(t, "test-key-1", pub)); !errors.Is(err, verify.ErrSignatureInvalid) {
		t.Errorf("injected unknown field: err = %v, want ErrSignatureInvalid", err)
	}
}

// Changing ONLY the signature field (to other valid 64-byte base64) fails — the
// signature field is excluded from the signing input but IS the value checked.
func TestManifestTamperedSignatureFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	raw := signed(t, validManifestMap(), priv)
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	bogus := make([]byte, ed25519.SignatureSize) // valid length, wrong value
	o["signature"], _ = json.Marshal(base64.StdEncoding.EncodeToString(bogus))
	if _, err := verify.Manifest(mustMarshal(t, o), keySetFor(t, "test-key-1", pub)); !errors.Is(err, verify.ErrSignatureInvalid) {
		t.Errorf("tampered signature: err = %v, want ErrSignatureInvalid", err)
	}
}

func TestManifestUnknownKeyIDFails(t *testing.T) {
	_, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	otherPub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	raw := signed(t, validManifestMap(), priv) // key_id "test-key-1"
	ks := keySetFor(t, "different-key", otherPub)
	if _, err := verify.Manifest(raw, ks); !errors.Is(err, trust.ErrUnknownKey) {
		t.Errorf("unknown key_id: err = %v, want trust.ErrUnknownKey", err)
	}
}

func TestManifestEmbeddedRevokedKeyIDFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	// k1 revoked, plus an active k2 so the set is valid.
	otherPub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	ks, err := trusttest.KeySet(
		trust.KeyEntry{KeyID: "test-key-1", PublicKey: pub, Revoked: true},
		trust.KeyEntry{KeyID: "test-key-2", PublicKey: otherPub},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verify.Manifest(signed(t, validManifestMap(), priv), ks); !errors.Is(err, trust.ErrRevokedKey) {
		t.Errorf("embedded-revoked key_id: err = %v, want trust.ErrRevokedKey", err)
	}
}

// AC-23: a key_id listed in the manifest's OWN key_revocation_list is rejected.
func TestManifestRevokedViaManifestListFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	m := validManifestMap()
	m["key_revocation_list"] = []any{"test-key-1"} // revoke the signing key in-manifest
	raw := signed(t, m, priv)                      // signed AFTER setting the list (valid signature)
	if _, err := verify.Manifest(raw, keySetFor(t, "test-key-1", pub)); !errors.Is(err, trust.ErrRevokedKey) {
		t.Errorf("manifest-revoked key_id: err = %v, want trust.ErrRevokedKey", err)
	}
}

// A revocation list that does NOT contain the signing key still verifies (rotation:
// the manifest retires an OLD key while signed by the active one).
func TestManifestRevocationListOtherKeyStillVerifies(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	m := validManifestMap()
	m["key_revocation_list"] = []any{"some-old-key"} // not the signer
	if _, err := verify.Manifest(signed(t, m, priv), keySetFor(t, "test-key-1", pub)); err != nil {
		t.Errorf("revoking an unrelated key must not block verification: %v", err)
	}
}

func TestManifestMalformedSignatureFails(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	build := func(sigVal string) []byte {
		raw := signed(t, validManifestMap(), priv)
		var o map[string]json.RawMessage
		_ = json.Unmarshal(raw, &o)
		o["signature"], _ = json.Marshal(sigVal)
		return mustMarshal(t, o)
	}
	ks := keySetFor(t, "test-key-1", pub)
	// not base64
	if _, err := verify.Manifest(build("@@@not-base64@@@"), ks); !errors.Is(err, verify.ErrMalformedSignature) {
		t.Errorf("non-base64 signature: err = %v, want ErrMalformedSignature", err)
	}
	// valid base64, wrong length (10 bytes != 64)
	short := base64.StdEncoding.EncodeToString(make([]byte, 10))
	if _, err := verify.Manifest(build(short), ks); !errors.Is(err, verify.ErrMalformedSignature) {
		t.Errorf("wrong-length signature: err = %v, want ErrMalformedSignature", err)
	}
}

// key_id matches but the trusted key is the WRONG key -> signature does not verify.
func TestManifestInvalidSignatureWrongKey(t *testing.T) {
	_, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	wrongPub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(2))
	raw := signed(t, validManifestMap(), priv) // key_id "test-key-1", signed by priv(seed1)
	ks := keySetFor(t, "test-key-1", wrongPub) // same key_id, different public key
	if _, err := verify.Manifest(raw, ks); !errors.Is(err, verify.ErrSignatureInvalid) {
		t.Errorf("wrong key: err = %v, want ErrSignatureInvalid", err)
	}
}

func TestManifestMissingFieldsRejected(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	ks := keySetFor(t, "test-key-1", pub)
	for _, field := range []string{"key_id", "signature"} {
		raw := signed(t, validManifestMap(), priv)
		var o map[string]json.RawMessage
		_ = json.Unmarshal(raw, &o)
		delete(o, field)
		if _, err := verify.Manifest(mustMarshal(t, o), ks); !errors.Is(err, verify.ErrManifestInvalid) {
			t.Errorf("missing %q: err = %v, want ErrManifestInvalid", field, err)
		}
	}
}

func TestManifestNilKeySetFailsClosed(t *testing.T) {
	_, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	if _, err := verify.Manifest(signed(t, validManifestMap(), priv), nil); !errors.Is(err, verify.ErrNoTrustAnchor) {
		t.Errorf("nil key set: err = %v, want ErrNoTrustAnchor", err)
	}
}

// A duplicate top-level key (inherited manifest edge case) is rejected at the
// canonical-signing-input step before any signature is accepted.
func TestManifestDuplicateKeyRejected(t *testing.T) {
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	// Hand-craft a JSON object with a duplicated top-level key. json decode is
	// last-wins (Parse succeeds) but CanonicalSigningInput rejects duplicates.
	dup := `{"schema_version":1,"target_version":"1.2.0","target_version":"1.2.0",` +
		`"min_supported_version":"1.0.0","artifacts":[{"platform":"linux","arch":"amd64",` +
		`"url":"https://x/y","size_bytes":1,"sha256":"` + hex64 + `","blake3":"` + hex64 + `"}],` +
		`"key_id":"test-key-1","key_revocation_list":[],"signature":"AAAA"}`
	_ = priv
	if _, err := verify.Manifest([]byte(dup), keySetFor(t, "test-key-1", pub)); !errors.Is(err, verify.ErrManifestInvalid) {
		t.Errorf("duplicate top-level key: err = %v, want ErrManifestInvalid", err)
	}
}

// ---- artifact dual-hash verification -----------------------------------------

func artifactFor(data []byte, sha, b3 string) manifest.Artifact {
	return manifest.Artifact{Platform: "linux", Arch: "amd64", URL: "https://x", SizeBytes: int64(len(data)), SHA256: sha, BLAKE3: b3}
}

func realDigests(data []byte) (sha, b3 string) {
	s := sha256.Sum256(data)
	b := blake3.Sum256(data)
	return hex.EncodeToString(s[:]), hex.EncodeToString(b[:])
}

func TestArtifactValid(t *testing.T) {
	data := []byte("the-agent-binary-content-v1.2.0")
	sha, b3 := realDigests(data)
	if err := verify.Artifact(artifactFor(data, sha, b3), bytes.NewReader(data)); err != nil {
		t.Errorf("matching dual hash rejected: %v", err)
	}
}

func TestArtifactSHA256Mismatch(t *testing.T) {
	data := []byte("content")
	_, b3 := realDigests(data)
	if err := verify.Artifact(artifactFor(data, hex64, b3), bytes.NewReader(data)); !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("sha256 mismatch: err = %v, want ErrHashMismatch", err)
	}
}

func TestArtifactBLAKE3Mismatch(t *testing.T) {
	data := []byte("content")
	sha, _ := realDigests(data)
	if err := verify.Artifact(artifactFor(data, sha, hex64), bytes.NewReader(data)); !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("blake3 mismatch: err = %v, want ErrHashMismatch", err)
	}
}

func TestArtifactBothRequired(t *testing.T) {
	data := []byte("content")
	// Both wrong -> mismatch. (Each-alone-wrong is covered above; both must match.)
	if err := verify.Artifact(artifactFor(data, hex64, hex64), bytes.NewReader(data)); !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("both wrong: err = %v, want ErrHashMismatch", err)
	}
	// Hash of DIFFERENT bytes than the digests describe -> mismatch.
	other := []byte("different-bytes")
	sha, b3 := realDigests(data)
	if err := verify.Artifact(artifactFor(data, sha, b3), bytes.NewReader(other)); !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("wrong bytes: err = %v, want ErrHashMismatch", err)
	}
}

func TestArtifactMalformedExpectedDigest(t *testing.T) {
	data := []byte("content")
	// A non-hex / wrong-length expected digest is a manifest-invalid (defensive;
	// a real verified manifest would already have schema-valid hashes).
	if err := verify.Artifact(artifactFor(data, "nothex", "nothex"), bytes.NewReader(data)); !errors.Is(err, verify.ErrManifestInvalid) {
		t.Errorf("malformed expected digest: err = %v, want ErrManifestInvalid", err)
	}
}
