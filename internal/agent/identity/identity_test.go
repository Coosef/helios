package identity_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
)

var (
	reFingerprint = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reRawMAC      = regexp.MustCompile(`([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}`)
)

func openMgr(t *testing.T, dir string, prot state.Protector) (*identity.Manager, *state.Store) {
	t.Helper()
	s, err := state.Open(state.Options{Dir: dir, Protector: prot})
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	m, err := identity.New(s)
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	return m, s
}

func TestNewRejectsNilStore(t *testing.T) {
	if _, err := identity.New(nil); err == nil {
		t.Error("New(nil) must error")
	}
}

func TestEnsureDeviceGUIDStableAndPersistent(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()

	m, s := openMgr(t, dir, prot)
	g1, err := m.EnsureDeviceGUID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(g1); err != nil {
		t.Errorf("device guid is not a valid UUID: %q", g1)
	}
	g2, _ := m.EnsureDeviceGUID()
	if g1 != g2 {
		t.Errorf("device guid not stable: %q != %q", g1, g2)
	}
	_ = s.Close()

	// Persists across a reopen.
	m2, s2 := openMgr(t, dir, prot)
	defer func() { _ = s2.Close() }()
	if g3, _ := m2.EnsureDeviceGUID(); g3 != g1 {
		t.Errorf("device guid did not persist: %q != %q", g3, g1)
	}
}

func TestEnsureKeyPairStableECDSAP256AndPersistent(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()

	m, s := openMgr(t, dir, prot)
	k1, err := m.EnsureKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if k1.Curve != elliptic.P256() {
		t.Errorf("key curve = %v, want P-256", k1.Curve.Params().Name)
	}
	k2, _ := m.EnsureKeyPair()
	if k1.D.Cmp(k2.D) != 0 {
		t.Error("keypair not stable within a manager")
	}
	_ = s.Close()

	// Persists across reopen (same protector instance).
	m2, s2 := openMgr(t, dir, prot)
	defer func() { _ = s2.Close() }()
	k3, err := m2.EnsureKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if k1.D.Cmp(k3.D) != 0 {
		t.Error("keypair did not persist across reopen")
	}
}

func TestEnsureMaterialValidCSRAndFingerprint(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	m, s := openMgr(t, dir, prot)
	defer func() { _ = s.Close() }()

	mat, err := m.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(mat.DeviceGUID); err != nil {
		t.Errorf("DeviceGUID invalid: %q", mat.DeviceGUID)
	}
	if !reFingerprint.MatchString(mat.SPKIFingerprint) {
		t.Errorf("SPKIFingerprint format = %q", mat.SPKIFingerprint)
	}

	// CSR parses, self-signature verifies, key is ECDSA P-256.
	block, _ := pem.Decode(mat.CSRPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("CSR PEM invalid: %q", string(mat.CSRPEM))
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("parse CSR: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature invalid: %v", err)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		t.Errorf("CSR public key is not ECDSA P-256: %T", csr.PublicKey)
	}
	if csr.Subject.CommonName != mat.DeviceGUID {
		t.Errorf("CSR CN = %q, want device guid %q", csr.Subject.CommonName, mat.DeviceGUID)
	}
	// The CSR public key must match the reported fingerprint.
	fp, _ := identity.SPKIFingerprint(csr.PublicKey)
	if fp != mat.SPKIFingerprint {
		t.Errorf("CSR pubkey fingerprint %q != material fingerprint %q", fp, mat.SPKIFingerprint)
	}
}

func TestSPKIFingerprintDeterministicAndCorrect(t *testing.T) {
	key, _ := identity.GenerateKey()
	f1, err := identity.SPKIFingerprint(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	f2, _ := identity.SPKIFingerprint(key.Public())
	if f1 != f2 {
		t.Error("SPKIFingerprint not deterministic")
	}
	// Equals sha256 of the SubjectPublicKeyInfo DER.
	spki, _ := x509.MarshalPKIXPublicKey(key.Public())
	sum := sha256.Sum256(spki)
	if want := "sha256:" + hex.EncodeToString(sum[:]); f1 != want {
		t.Errorf("fingerprint = %q, want %q", f1, want)
	}
}

func TestPKCS8RoundTripAndRejectsWrongKeys(t *testing.T) {
	key, _ := identity.GenerateKey()
	der, err := identity.MarshalPrivateKeyPKCS8(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := identity.ParsePrivateKeyPKCS8(der)
	if err != nil || got.D.Cmp(key.D) != 0 {
		t.Fatalf("round-trip failed: %v", err)
	}

	// Wrong curve (P-384) rejected.
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der384, _ := x509.MarshalPKCS8PrivateKey(p384)
	if _, err := identity.ParsePrivateKeyPKCS8(der384); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("P-384 key = %v, want ErrInvalidKey", err)
	}
	// Non-ECDSA (ed25519) rejected.
	_, edPriv, _ := ed25519.GenerateKey(rand.Reader)
	derEd, _ := x509.MarshalPKCS8PrivateKey(edPriv)
	if _, err := identity.ParsePrivateKeyPKCS8(derEd); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("ed25519 key = %v, want ErrInvalidKey", err)
	}
}

func TestHardwareSignalsArePrivacyPreserving(t *testing.T) {
	hw := identity.CollectHardwareSignals()
	b, _ := json.Marshal(hw)
	js := string(b)

	// No raw MAC may appear anywhere in the serialized signals.
	if reRawMAC.MatchString(js) {
		t.Errorf("raw MAC leaked into hardware signals: %s", js)
	}
	// Every populated hash field must be sha256:<hex>.
	for name, v := range map[string]string{
		"machine_guid":  hw.MachineGUIDSHA256,
		"disk_serial":   hw.PrimaryDiskSerialSHA256,
		"first_nic_mac": hw.FirstNICMACSHA256,
	} {
		if v != "" && !reFingerprint.MatchString(v) {
			t.Errorf("%s hash format = %q", name, v)
		}
	}
	// OS must always be a valid OpenAPI enum value (windows|linux), or empty
	// (omitted) on unsupported dev hosts — never an out-of-enum value like "darwin".
	if hw.OS != "" && hw.OS != "windows" && hw.OS != "linux" {
		t.Errorf("OS = %q, must be empty or one of [windows, linux]", hw.OS)
	}
}

func TestMaterialContainsNoPrivateKeyOrRawPII(t *testing.T) {
	dir := t.TempDir()
	prot, _ := state.NewInsecureTestProtector()
	m, s := openMgr(t, dir, prot)
	defer func() { _ = s.Close() }()

	mat, _ := m.Ensure()
	b, _ := json.Marshal(mat)
	js := strings.ToLower(string(b))
	for _, banned := range []string{"private", "\"d\":", "begin private key", reRawMACFind(string(b))} {
		if banned != "" && strings.Contains(js, strings.ToLower(banned)) {
			t.Errorf("material leaked sensitive content %q", banned)
		}
	}
	if reRawMAC.MatchString(string(b)) {
		t.Error("material leaked a raw MAC")
	}
}

func reRawMACFind(s string) string { return reRawMAC.FindString(s) }

func TestGenerateCSRRejectsEmptyGUID(t *testing.T) {
	key, _ := identity.GenerateKey()
	if _, err := identity.GenerateCSR(key, ""); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("GenerateCSR(empty guid) = %v, want ErrInvalidKey", err)
	}
}

func TestEnsureKeyPairFailsClosedWithoutProtector(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows default protector is DPAPI")
	}
	dir := t.TempDir()
	m, s := openMgr(t, dir, nil) // nil protector -> unsupported on non-windows
	defer func() { _ = s.Close() }()

	if _, err := m.EnsureKeyPair(); !errors.Is(err, state.ErrUnsupportedProtection) {
		t.Errorf("EnsureKeyPair = %v, want ErrUnsupportedProtection (no plaintext key)", err)
	}
	// Ensure() must also fail closed (no plaintext key path).
	if _, err := m.Ensure(); !errors.Is(err, state.ErrUnsupportedProtection) {
		t.Errorf("Ensure = %v, want ErrUnsupportedProtection", err)
	}
	// Nothing was stored.
	if _, err := s.GetSecret(state.SecretPrivateKey); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("a key must not have been stored, got %v", err)
	}
}
