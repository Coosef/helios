package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

const (
	pemTypePKCS8 = "PRIVATE KEY"         // PKCS#8
	pemTypeCSR   = "CERTIFICATE REQUEST" // PKCS#10
)

// GenerateKey creates a new ECDSA P-256 identity private key (the frozen agent
// key algorithm — ADR-003 addendum). The key becomes the mTLS identity later.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generating ECDSA P-256 key: %w", err)
	}
	return key, nil
}

// MarshalPrivateKeyPKCS8 encodes key as PKCS#8 DER (stored Protector-wrapped).
func MarshalPrivateKeyPKCS8(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("identity: marshaling PKCS#8 private key: %w", err)
	}
	return der, nil
}

// ParsePrivateKeyPKCS8 decodes a PKCS#8 DER key and asserts it is ECDSA P-256.
func ParsePrivateKeyPKCS8(der []byte) (*ecdsa.PrivateKey, error) {
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("identity: parsing PKCS#8 private key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: stored key is not ECDSA", ErrInvalidKey)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: stored key is not P-256", ErrInvalidKey)
	}
	return key, nil
}

// SPKIFingerprint returns sha256:<hex> of the public key's SubjectPublicKeyInfo
// DER (the frozen credential fingerprint — ADR-003 addendum).
func SPKIFingerprint(pub crypto.PublicKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("identity: marshaling SubjectPublicKeyInfo: %w", err)
	}
	sum := sha256.Sum256(spki)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// GenerateCSR builds a PEM-encoded PKCS#10 CSR for key, signed ECDSA-with-SHA256,
// with CommonName = deviceGUID. Tenant/region are intentionally NOT asserted in
// the CSR: the server binds them authoritatively at enrollment (ADR-003). The CSR
// only proves possession of the private key.
func GenerateCSR(key *ecdsa.PrivateKey, deviceGUID string) ([]byte, error) {
	if deviceGUID == "" {
		return nil, fmt.Errorf("%w: empty device guid for CSR", ErrInvalidKey)
	}
	tmpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: deviceGUID},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return nil, fmt.Errorf("identity: creating CSR: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: pemTypeCSR, Bytes: der}), nil
}
