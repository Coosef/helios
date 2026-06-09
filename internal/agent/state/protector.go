package state

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

// InsecureTestProtector is a TEST-ONLY Protector. It uses an in-memory random
// AES-256-GCM key (lost on process restart) and provides NO durable or
// production-grade protection. It exists solely so tests on platforms without a
// real protector (i.e. non-Windows) can exercise the secret wrap/unwrap path.
//
// NEVER use it in production. The platform default protector (DPAPI on Windows,
// fail-closed elsewhere) is what production must use; it is selected automatically
// when Options.Protector is nil.
type InsecureTestProtector struct {
	gcm cipher.AEAD
}

// NewInsecureTestProtector returns a test protector with a fresh random key.
// Reuse the SAME instance across Open calls within a test to round-trip secrets
// (the key is in memory only and is not shared between instances).
func NewInsecureTestProtector() (*InsecureTestProtector, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("state: generating test key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &InsecureTestProtector{gcm: gcm}, nil
}

// Name identifies the protector (stored alongside each secret).
func (p *InsecureTestProtector) Name() string { return "insecure-test" }

// Protect returns nonce||ciphertext.
func (p *InsecureTestProtector) Protect(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, p.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("state: generating nonce: %w", err)
	}
	return p.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Unprotect opens nonce||ciphertext. Tampering fails closed via the GCM auth tag.
func (p *InsecureTestProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	ns := p.gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("state: ciphertext too short")
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	plaintext, err := p.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("state: gcm open: %w", err)
	}
	return plaintext, nil
}
