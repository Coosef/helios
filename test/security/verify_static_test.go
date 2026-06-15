package security

import (
	"os"
	"strings"
	"testing"
)

// AC-23 static guard: the manifest signature verifier must call the REAL Ed25519
// primitive and must NOT contain a `return true` shortcut (a verification stub),
// and the embedded trust material must be public-key-only.
func TestNeg_NoReturnTrueVerificationStub(t *testing.T) {
	src, err := os.ReadFile("../../internal/updater/verify/verify.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "ed25519.Verify(") {
		t.Error("verify.go must call the real ed25519.Verify primitive (no shortcut)")
	}
	if strings.Contains(s, "return true") {
		t.Error("verify.go must not contain a 'return true' verification stub")
	}

	// AC-23/AC-35: the embedded keyset is PUBLIC material only (no private key).
	ks, err := os.ReadFile("../../internal/updater/trust/keyset.json")
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(ks))
	if strings.Contains(string(ks), "PRIVATE KEY") || strings.Contains(low, "private_key") || strings.Contains(low, "privatekey") {
		t.Error("the embedded update keyset must contain PUBLIC keys only")
	}
}
