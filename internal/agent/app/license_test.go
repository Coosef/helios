package app

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/license"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
)

// licenseTestKey derives the license TEST private key from the fixed seed whose
// PUBLIC half is committed in internal/agent/license/license_keyset.json.
func licenseTestKey() ed25519.PrivateKey {
	var seed [32]byte
	for i := range seed {
		seed[i] = 0x4C
	}
	return ed25519.NewKeyFromSeed(seed[:])
}

func licenseClaims() map[string]any {
	return map[string]any{
		"schema_version": 1, "license_id": "lic_app", "tenant_id": "tnt_app", "parent_org_id": "",
		"plan": "pro", "seats": 10, "quota_bytes": int64(1 << 30),
		"issued_at": "2026-01-01T00:00:00Z", "not_before": "2026-01-01T00:00:00Z",
		"not_after": "2999-01-01T00:00:00Z", "key_id": "beyz-license-test-2026-01",
	}
}

// signedLicenseBlob produces the persisted license-blob secret (the base64 envelope
// over a signed token), exactly as the enrollment use-case would store it.
func signedLicenseBlob(t *testing.T, claims map[string]any) []byte {
	t.Helper()
	raw, _ := json.Marshal(claims)
	ci, err := license.CanonicalSigningInput(raw)
	if err != nil {
		t.Fatalf("canonical signing input: %v", err)
	}
	claims["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(licenseTestKey(), ci))
	token, _ := json.Marshal(claims)
	env, _ := json.Marshal(map[string]any{
		"encoding": "base64", "signature_alg": "ed25519", "value": base64.StdEncoding.EncodeToString(token),
	})
	return env
}

// openWithLicense persists blob (and optional tenant) into a fresh state store, then
// constructs an App over it (the same protector instance keeps the wrap key).
func openWithLicense(t *testing.T, blob []byte, tenant string) (*App, string, state.Protector) {
	t.Helper()
	dir := t.TempDir()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	if blob != nil {
		if err := st.PutSecret(state.SecretLicenseBlob, blob); err != nil {
			t.Fatal(err)
		}
	}
	if tenant != "" {
		if err := st.Put(state.KeyTenantID, []byte(tenant)); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()

	app, err := New(Options{Config: testConfig(t), StateDir: dir, Protector: prot, BootstrapPins: []string{dummyPin}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app, dir, prot
}

// A validly-signed, in-window, matching-tenant license is classified Valid (advisory).
func TestLicenseAdvisoryValid(t *testing.T) {
	app, _, _ := openWithLicense(t, signedLicenseBlob(t, licenseClaims()), "tnt_app")
	defer app.Close()
	lic := app.License()
	if lic == nil || lic.Status != license.StatusValid {
		t.Fatalf("status = %v, want valid", lic)
	}
	if lic.Claims == nil || lic.Claims.Plan != "pro" || lic.Claims.Seats != 10 {
		t.Errorf("claims = %+v", lic.Claims)
	}
}

// A tampered license blob is classified SignatureInvalid, startup STILL succeeds
// (advisory), and a license.signature_invalid hash-chained audit event is emitted.
func TestLicenseAdvisoryTamperedEmitsAuditButDoesNotBlock(t *testing.T) {
	tampered := tamperBlob(t, signedLicenseBlob(t, licenseClaims()))
	app, dir, prot := openWithLicense(t, tampered, "")
	if app.License().Status != license.StatusSignatureInvalid {
		t.Errorf("status = %s, want signature_invalid", app.License().Status)
	}
	_ = app.Close()

	// Reopen the store and confirm the audit event landed in the hash-chained spool.
	st, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	recs, err := st.AuditAppender().Records()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recs {
		if r.EventType == "license.signature_invalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a license.signature_invalid audit record (got %d records)", len(recs))
	}
}

// A missing license blob (the Sprint-1 default) is tolerated: status Missing, no error.
func TestLicenseAdvisoryMissingTolerated(t *testing.T) {
	app, _, _ := openWithLicense(t, nil, "")
	defer app.Close()
	if app.License().Status != license.StatusMissing {
		t.Errorf("status = %s, want missing", app.License().Status)
	}
}

func tamperBlob(t *testing.T, blob []byte) []byte {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(blob, &env); err != nil {
		t.Fatal(err)
	}
	token, _ := base64.StdEncoding.DecodeString(env["value"].(string))
	var claims map[string]any
	if err := json.Unmarshal(token, &claims); err != nil {
		t.Fatal(err)
	}
	claims["seats"] = 99999 // tamper a signed claim -> breaks the signature
	newToken, _ := json.Marshal(claims)
	env["value"] = base64.StdEncoding.EncodeToString(newToken)
	out, _ := json.Marshal(env)
	return out
}
