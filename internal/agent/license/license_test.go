package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
)

const testKeyID = "beyz-license-test-2026-01"

// testKey derives the license TEST keypair from the fixed seed whose PUBLIC half is
// committed in license_keyset.json. Tests sign with the private half; production
// never holds it (it lives in the KMS / CI secret manager).
func testKey() ed25519.PrivateKey {
	var seed [32]byte
	for i := range seed {
		seed[i] = 0x4C
	}
	return ed25519.NewKeyFromSeed(seed[:])
}

func validClaims() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"license_id":     "lic_01HX",
		"tenant_id":      "tnt_x",
		"parent_org_id":  "",
		"plan":           "pro",
		"seats":          25,
		"quota_bytes":    int64(5 << 30),
		"issued_at":      "2026-01-01T00:00:00Z",
		"not_before":     "2026-01-01T00:00:00Z",
		"not_after":      "2030-01-01T00:00:00Z",
		"key_id":         testKeyID,
	}
}

// signToken signs a claims map and returns the token JSON (claims + base64 Ed25519
// signature over the JCS canonical form), mirroring the offline signing ceremony.
func signToken(t *testing.T, claims map[string]any, priv ed25519.PrivateKey) []byte {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := CanonicalSigningInput(raw)
	if err != nil {
		t.Fatalf("canonical signing input: %v", err)
	}
	claims["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, ci))
	out, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func embeddedKeys(t *testing.T) *trust.KeySet {
	t.Helper()
	ks, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	return ks
}

// The committed license_keyset.json public key MUST match the test seed, or every
// signing test would be verifying against the wrong key (and a real keyset drift
// would go unnoticed).
func TestEmbeddedKeysetMatchesTestSeed(t *testing.T) {
	pub, err := embeddedKeys(t).Key(testKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.Equal(testKey().Public().(ed25519.PublicKey)) {
		t.Error("committed license_keyset.json public key does not match the test seed (drift)")
	}
}

func TestVerifyValid(t *testing.T) {
	c, err := Verify(signToken(t, validClaims(), testKey()), embeddedKeys(t))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.TenantID != "tnt_x" || c.Plan != "pro" || c.Seats != 25 || c.QuotaBytes != 5<<30 || c.KeyID != testKeyID {
		t.Errorf("claims = %+v", c)
	}
}

func TestVerifyNilKeySetFailsClosed(t *testing.T) {
	if _, err := Verify(signToken(t, validClaims(), testKey()), nil); !errors.Is(err, ErrNoTrustAnchor) {
		t.Errorf("err = %v, want ErrNoTrustAnchor", err)
	}
}

func TestVerifyTamperedFailsClosed(t *testing.T) {
	token := signToken(t, validClaims(), testKey())
	var m map[string]any
	if err := json.Unmarshal(token, &m); err != nil {
		t.Fatal(err)
	}
	m["seats"] = 9999 // tamper a signed claim AFTER signing
	tampered, _ := json.Marshal(m)
	if _, err := Verify(tampered, embeddedKeys(t)); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestVerifyUnknownKeyID(t *testing.T) {
	cl := validClaims()
	cl["key_id"] = "no-such-key"
	if _, err := Verify(signToken(t, cl, testKey()), embeddedKeys(t)); !errors.Is(err, trust.ErrUnknownKey) {
		t.Errorf("err = %v, want trust.ErrUnknownKey", err)
	}
}

func TestVerifyWrongSigningKeyFailsClosed(t *testing.T) {
	var seed [32]byte
	for i := range seed {
		seed[i] = 0x99 // a different key, but the token still claims the embedded key_id
	}
	if _, err := Verify(signToken(t, validClaims(), ed25519.NewKeyFromSeed(seed[:])), embeddedKeys(t)); !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestVerifyUnsupportedSchemaVersion(t *testing.T) {
	cl := validClaims()
	cl["schema_version"] = 2
	if _, err := Verify(signToken(t, cl, testKey()), embeddedKeys(t)); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Errorf("err = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

func TestVerifyMalformedSignature(t *testing.T) {
	cl := validClaims()
	cl["signature"] = "not-base64!!!"
	bad, _ := json.Marshal(cl)
	if _, err := Verify(bad, embeddedKeys(t)); !errors.Is(err, ErrMalformedSignature) {
		t.Errorf("non-base64 sig err = %v, want ErrMalformedSignature", err)
	}
	cl["signature"] = base64.StdEncoding.EncodeToString([]byte("too-short"))
	short, _ := json.Marshal(cl)
	if _, err := Verify(short, embeddedKeys(t)); !errors.Is(err, ErrMalformedSignature) {
		t.Errorf("short sig err = %v, want ErrMalformedSignature", err)
	}
}

func TestVerifyMalformedToken(t *testing.T) {
	cases := map[string]string{
		"not json":      `{bad`,
		"not object":    `["a"]`,
		"dup top key":   `{"schema_version":1,"schema_version":1,"key_id":"beyz-license-test-2026-01","signature":""}`,
		"trailing data": `{"schema_version":1,"signature":""} junk`,
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify([]byte(tok), embeddedKeys(t)); err == nil {
				t.Errorf("want error for %q", name)
			}
		})
	}
}

func TestForwardCompatUnknownSignedFields(t *testing.T) {
	cl := validClaims()
	cl["future_field"] = "ignored-but-signed"
	if _, err := Verify(signToken(t, cl, testKey()), embeddedKeys(t)); err != nil {
		t.Errorf("unknown signed fields must still verify (forward compat): %v", err)
	}
}

func TestDecodeToken(t *testing.T) {
	mkBlob := func(env map[string]any) []byte { b, _ := json.Marshal(env); return b }
	token := []byte(`{"schema_version":1}`)
	val := base64.StdEncoding.EncodeToString(token)

	if _, err := DecodeToken(nil); !errors.Is(err, ErrNoLicense) {
		t.Errorf("absent: %v", err)
	}
	if _, err := DecodeToken(mkBlob(map[string]any{"encoding": "base64"})); !errors.Is(err, ErrNoLicense) {
		t.Errorf("empty value: %v", err)
	}
	if _, err := DecodeToken([]byte(`{bad`)); !errors.Is(err, ErrMalformed) {
		t.Errorf("bad envelope: %v", err)
	}
	if _, err := DecodeToken(mkBlob(map[string]any{"encoding": "hex", "value": val})); !errors.Is(err, ErrMalformed) {
		t.Errorf("bad encoding: %v", err)
	}
	if _, err := DecodeToken(mkBlob(map[string]any{"signature_alg": "rsa", "value": val})); !errors.Is(err, ErrMalformed) {
		t.Errorf("bad alg: %v", err)
	}
	if _, err := DecodeToken(mkBlob(map[string]any{"value": "!!!notb64"})); !errors.Is(err, ErrMalformed) {
		t.Errorf("bad b64: %v", err)
	}
	got, err := DecodeToken(mkBlob(map[string]any{"encoding": "base64", "signature_alg": "ed25519", "value": val}))
	if err != nil || string(got) != string(token) {
		t.Errorf("valid decode = %q, %v", got, err)
	}
}

func TestEvaluateClassification(t *testing.T) {
	now := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	ks := embeddedKeys(t)
	valid := signToken(t, validClaims(), testKey())

	if r := Evaluate(valid, ks, now, "tnt_x"); r.Status != StatusValid || r.Claims == nil {
		t.Errorf("valid: status=%s claims=%v", r.Status, r.Claims)
	}
	t.Run("expired", func(t *testing.T) {
		cl := validClaims()
		cl["not_after"] = "2020-01-01T00:00:00Z"
		r := Evaluate(signToken(t, cl, testKey()), ks, now, "tnt_x")
		if r.Status != StatusExpired || r.Claims == nil {
			t.Errorf("status=%s", r.Status)
		}
	})
	t.Run("not yet valid", func(t *testing.T) {
		cl := validClaims()
		cl["not_before"] = "2099-01-01T00:00:00Z"
		if r := Evaluate(signToken(t, cl, testKey()), ks, now, "tnt_x"); r.Status != StatusNotYetValid {
			t.Errorf("status=%s", r.Status)
		}
	})
	t.Run("tenant mismatch", func(t *testing.T) {
		if r := Evaluate(valid, ks, now, "tnt_OTHER"); r.Status != StatusTenantMismatch || r.Claims == nil {
			t.Errorf("status=%s", r.Status)
		}
	})
	t.Run("no agent tenant is not a mismatch", func(t *testing.T) {
		if r := Evaluate(valid, ks, now, ""); r.Status != StatusValid {
			t.Errorf("status=%s", r.Status)
		}
	})
	t.Run("malformed not_after is ignored (advisory)", func(t *testing.T) {
		cl := validClaims()
		cl["not_after"] = "garbage"
		if r := Evaluate(signToken(t, cl, testKey()), ks, now, "tnt_x"); r.Status != StatusValid {
			t.Errorf("status=%s", r.Status)
		}
	})
	t.Run("signature invalid", func(t *testing.T) {
		var m map[string]any
		_ = json.Unmarshal(valid, &m)
		m["seats"] = 1
		bad, _ := json.Marshal(m)
		r := Evaluate(bad, ks, now, "tnt_x")
		if r.Status != StatusSignatureInvalid || r.Claims != nil {
			t.Errorf("status=%s claims=%v", r.Status, r.Claims)
		}
	})
}
