package license

import (
	_ "embed"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
)

// embeddedKeySetJSON is the compile-time LICENSE trust anchor: the public-key SET
// that signs license blobs. It is SEPARATE from the update trust anchor
// (internal/updater/trust/keyset.json) — a license-signing key compromise must not
// equal an update-signing key compromise (different custody, different rotation).
// Sprint 1 ships a TEST public key; the production build embeds the real
// KMS/HSM-backed license key(s). Public-key only — no private key is, or is
// derivable from, this file (AC-35). The registry format is shared with the update
// keyset, so the hardened trust.ParseKeySet loader is reused (only the key material
// differs).
//
//go:embed license_keyset.json
var embeddedKeySetJSON []byte

var (
	embeddedKeySet *trust.KeySet
	embeddedErr    error
)

func init() {
	embeddedKeySet, embeddedErr = trust.ParseKeySet(embeddedKeySetJSON)
}

// Embedded returns the compile-time embedded license trust set, or a fail-closed
// error if the embedded keyset is missing, malformed, empty, or fully revoked. A
// non-nil error means "no license trust anchor" — verification cannot succeed, which
// (advisory) is recorded but does not block the agent.
func Embedded() (*trust.KeySet, error) {
	if embeddedErr != nil {
		return nil, embeddedErr
	}
	return embeddedKeySet, nil
}
