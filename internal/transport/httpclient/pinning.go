package httpclient

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const pinPrefix = "sha256:"

// Pin-related errors.
var (
	// ErrNoPins is returned when no SPKI pins are configured. The control channel
	// is fail-closed: an unpinned client is never constructed (SEC-4).
	ErrNoPins = errors.New("httpclient: no SPKI pins configured")
	// ErrInvalidPin indicates a malformed pin (must be "sha256:<64-hex>").
	ErrInvalidPin = errors.New("httpclient: invalid SPKI pin format")
	// ErrPinMismatch indicates the server presented a certificate whose public
	// key is not in the configured pin set (a MITM signal — SEC-4).
	ErrPinMismatch = errors.New("httpclient: server SPKI pin mismatch")
)

// PinFromSPKI returns the pin ("sha256:<hex>") of a DER-encoded
// SubjectPublicKeyInfo.
func PinFromSPKI(spkiDER []byte) string {
	sum := sha256.Sum256(spkiDER)
	return pinPrefix + hex.EncodeToString(sum[:])
}

// PinFromCertificate returns the SPKI pin of a certificate's public key.
func PinFromCertificate(cert *x509.Certificate) string {
	return PinFromSPKI(cert.RawSubjectPublicKeyInfo)
}

// normalizePins validates and indexes the pin set. Supplying multiple pins is
// how rotation is supported: keep the outgoing and incoming pins in the set with
// overlapping validity so a key rollover is never a fleet lockout (§0.5, REV-2).
func normalizePins(pins []string) (map[string]struct{}, error) {
	if len(pins) == 0 {
		return nil, ErrNoPins
	}
	set := make(map[string]struct{}, len(pins))
	for _, raw := range pins {
		p := strings.ToLower(strings.TrimSpace(raw))
		if !strings.HasPrefix(p, pinPrefix) || len(p) != len(pinPrefix)+64 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPin, raw)
		}
		if _, err := hex.DecodeString(p[len(pinPrefix):]); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPin, raw)
		}
		set[p] = struct{}{}
	}
	return set, nil
}

// pinVerifier returns a tls.Config.VerifyConnection callback that enforces SPKI
// pinning. It is the trust anchor for the control channel: the server's leaf
// public key MUST match a configured pin. Because the client runs with
// InsecureSkipVerify=true (the pin replaces CA-chain trust), this callback ALSO
// re-implements the two checks Go would otherwise skip — certificate validity
// window (expiry) and host identity — so they hold for every endpoint, including
// IP-literal targets where SNI (cs.ServerName) is empty. System-CA-only trust is
// intentionally NOT relied upon (SEC-4).
func pinVerifier(set map[string]struct{}, expectedHost string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("%w: server presented no certificate", ErrPinMismatch)
		}
		leaf := cs.PeerCertificates[0]

		// 1. SPKI pin — the trust anchor.
		if _, ok := set[PinFromCertificate(leaf)]; !ok {
			return fmt.Errorf("%w: server presented an unpinned public key", ErrPinMismatch)
		}

		// 2. Validity window. InsecureSkipVerify disables Go's own expiry check,
		//    so it must be enforced here (a pinned-but-expired cert is rejected).
		now := time.Now()
		if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
			return fmt.Errorf("%w: certificate is outside its validity window", ErrPinMismatch)
		}

		// 3. Host identity. cs.ServerName is the SNI — empty for IP-literal
		//    targets, where it falls back to the explicitly configured host. Fail
		//    closed when no host is available to verify against (never accept a
		//    pin-matched cert for an unverifiable host).
		host := cs.ServerName
		if host == "" {
			host = expectedHost
		}
		if host == "" {
			return fmt.Errorf("%w: cannot verify certificate host identity (no SNI or configured server name)", ErrPinMismatch)
		}
		if err := leaf.VerifyHostname(host); err != nil {
			return fmt.Errorf("%w: certificate not valid for %q: %v", ErrPinMismatch, host, err)
		}
		return nil
	}
}
