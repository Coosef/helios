package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func TestCanonicalStableAcrossKeyOrderAndWhitespace(t *testing.T) {
	a := `{"schema_version":1,"target_version":"1.2.0","min_supported_version":"1.0.0","key_id":"k","key_revocation_list":[],"artifacts":[{"platform":"windows","arch":"amd64","url":"https://x/y","size_bytes":1024,"sha256":"` + hex64 + `","blake3":"` + hex64 + `"}],"signature":"AAAA"}`
	b := `{
	  "signature": "DIFFERENT-SIGNATURE",
	  "artifacts": [ { "blake3":"` + hex64 + `", "sha256":"` + hex64 + `", "size_bytes":1024, "url":"https://x/y", "arch":"amd64", "platform":"windows" } ],
	  "key_revocation_list": [],
	  "key_id": "k",
	  "min_supported_version": "1.0.0",
	  "target_version": "1.2.0",
	  "schema_version": 1
	}`
	ca, err := manifest.CanonicalSigningInput([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	cb, err := manifest.CanonicalSigningInput([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Errorf("canonical form not stable:\n a=%s\n b=%s", ca, cb)
	}
}

func TestCanonicalExcludesSignature(t *testing.T) {
	in := `{"schema_version":1,"target_version":"1.0.0","min_supported_version":"1.0.0","artifacts":[],"key_id":"k","key_revocation_list":[],"signature":"SECRET-SIG-VALUE"}`
	c, err := manifest.CanonicalSigningInput([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c), "signature") || strings.Contains(string(c), "SECRET-SIG-VALUE") {
		t.Errorf("canonical signing input must exclude the signature field: %s", c)
	}
}

// The signing input must PRESERVE unknown fields, so a forward manifest carrying
// additional signed fields verifies identically for signer and verifier.
func TestCanonicalPreservesUnknownFields(t *testing.T) {
	in := `{"schema_version":1,"target_version":"1.0.0","min_supported_version":"1.0.0","artifacts":[],"key_id":"k","key_revocation_list":[],"signature":"s","future_field":"must-be-signed"}`
	c, err := manifest.CanonicalSigningInput([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c), "future_field") || !strings.Contains(string(c), "must-be-signed") {
		t.Errorf("unknown fields must be preserved in the signing input: %s", c)
	}
}

func TestCanonicalDeterministicAndIdempotent(t *testing.T) {
	in := `{"b":2,"a":1,"signature":"x"}`
	c1, err := manifest.CanonicalSigningInput([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	// Re-canonicalizing the canonical output (already signature-free) is stable.
	c2, err := manifest.CanonicalSigningInput(c1)
	if err != nil {
		t.Fatal(err)
	}
	if string(c1) != string(c2) {
		t.Errorf("canonicalization not idempotent: %s vs %s", c1, c2)
	}
	if string(c1) != `{"a":1,"b":2}` {
		t.Errorf("canonical = %s, want sorted keys without signature", c1)
	}
}

func TestCanonicalRejectsNonObject(t *testing.T) {
	for _, bad := range []string{`[1,2]`, `"str"`, `42`, `not json`} {
		if _, err := manifest.CanonicalSigningInput([]byte(bad)); !errors.Is(err, manifest.ErrCanonicalize) {
			t.Errorf("CanonicalSigningInput(%q) err = %v, want ErrCanonicalize", bad, err)
		}
	}
}

func TestCanonicalRejectsDuplicateTopLevelKey(t *testing.T) {
	// last-wins map collapse would hide this; RFC 8785 forbids duplicate names.
	in := `{"target_version":"1.0.0","target_version":"9.9.9","signature":"x"}`
	if _, err := manifest.CanonicalSigningInput([]byte(in)); !errors.Is(err, manifest.ErrCanonicalize) {
		t.Errorf("duplicate top-level key: err = %v, want ErrCanonicalize", err)
	}
}

func TestCanonicalRejectsDuplicateSignatureKey(t *testing.T) {
	in := `{"signature":"REAL","signature":"FAKE","a":1}`
	if _, err := manifest.CanonicalSigningInput([]byte(in)); !errors.Is(err, manifest.ErrCanonicalize) {
		t.Errorf("duplicate signature key: err = %v, want ErrCanonicalize", err)
	}
}

func TestCanonicalRejectsNestedDuplicateKey(t *testing.T) {
	in := `{"obj":{"k":1,"k":2},"signature":"x"}`
	if _, err := manifest.CanonicalSigningInput([]byte(in)); !errors.Is(err, manifest.ErrCanonicalize) {
		t.Errorf("nested duplicate key: err = %v, want ErrCanonicalize", err)
	}
}

func TestCanonicalRejectsTrailingData(t *testing.T) {
	in := `{"a":1,"signature":"x"} trailing-garbage`
	if _, err := manifest.CanonicalSigningInput([]byte(in)); !errors.Is(err, manifest.ErrCanonicalize) {
		t.Errorf("trailing data: err = %v, want ErrCanonicalize", err)
	}
}
