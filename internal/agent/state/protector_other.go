//go:build !windows

package state

// defaultProtector on non-Windows platforms fails closed: there is no
// production-grade secret protector yet (Linux keyring / TPM is Sprint 8+), so
// PutSecret returns ErrUnsupportedProtection rather than ever writing plaintext.
// Tests inject an InsecureTestProtector via Options.Protector.
func defaultProtector() Protector { return unsupportedProtector{} }

type unsupportedProtector struct{}

func (unsupportedProtector) Name() string { return "unsupported" }

func (unsupportedProtector) Protect([]byte) ([]byte, error) {
	return nil, ErrUnsupportedProtection
}

func (unsupportedProtector) Unprotect([]byte) ([]byte, error) {
	return nil, ErrUnsupportedProtection
}
