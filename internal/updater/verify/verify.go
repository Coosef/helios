// Package verify performs REAL, enforcing update-manifest verification (S1-T23,
// ADR-002): an Ed25519 signature over the RFC 8785 (JCS) canonical manifest using
// a build-time embedded public-key set selected by key_id, plus dual SHA-256 +
// BLAKE3 verification of a downloaded artifact against the SIGNED manifest. There
// is no return-true shortcut and no path that trusts an unsigned/unverified field.
//
// SCOPE (T23): signature + artifact-hash verification ONLY. It does NOT fetch a
// manifest (T24), decide anti-rollback (T24), download a binary (T25), swap (T25),
// or health-gate (T26). The caller MUST verify the manifest signature (Manifest)
// BEFORE trusting any field, then download the artifact and verify it (Artifact)
// BEFORE any swap — the expected hashes come only from the verified manifest, never
// from a header, filename, or unsigned source (AC-24/AC-29).
package verify

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/zeebo/blake3"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

var (
	// ErrNoTrustAnchor is returned when no key set is supplied (fail closed).
	ErrNoTrustAnchor = errors.New("verify: no trust anchor (nil key set)")
	// ErrManifestInvalid wraps a structural/canonicalization failure of the manifest
	// (including the duplicate-key and trailing-data rejections inherited from the
	// canonical signing input).
	ErrManifestInvalid = errors.New("verify: manifest invalid")
	// ErrMalformedSignature is returned when the signature is not base64 or not the
	// Ed25519 signature size.
	ErrMalformedSignature = errors.New("verify: malformed signature")
	// ErrSignatureInvalid is returned when the Ed25519 signature does not verify over
	// the canonical signing input under the selected trusted key.
	ErrSignatureInvalid = errors.New("verify: signature verification failed")
	// ErrHashMismatch is returned when an artifact's SHA-256 or BLAKE3 does not match
	// the signed manifest.
	ErrHashMismatch = errors.New("verify: artifact hash mismatch")
)

// Manifest parses rawManifest, then verifies the Ed25519 signature over its
// canonical signing input (the manifest minus the signature field) using the key
// selected by key_id from keys, honoring the manifest's key_revocation_list
// (ADR-002 #3). On success it returns the signature-verified manifest — the only
// trustworthy source of artifact hashes and versions.
//
// Order of rejection: nil key set -> structural/canonical invalidity -> malformed
// signature -> untrusted/unknown/revoked key_id -> invalid signature.
func Manifest(rawManifest []byte, keys *trust.KeySet) (*manifest.Manifest, error) {
	if keys == nil {
		return nil, ErrNoTrustAnchor
	}
	m, err := manifest.Parse(rawManifest)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	// Canonical signing input over the EXACT received bytes (preserves unknown
	// fields, rejects duplicate keys / trailing data) — the bytes the signature
	// commits to.
	signingInput, err := manifest.CanonicalSigningInput(rawManifest)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64: %v", ErrMalformedSignature, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: %d bytes, want %d", ErrMalformedSignature, len(sig), ed25519.SignatureSize)
	}
	// Select the key: present AND not embedded-revoked AND not in the manifest's
	// revocation list. Propagates trust.ErrUnknownKey / trust.ErrRevokedKey.
	pub, err := keys.TrustedKey(m.KeyID, m.KeyRevocationList)
	if err != nil {
		return nil, fmt.Errorf("verify: untrusted key_id: %w", err)
	}
	if !ed25519.Verify(pub, signingInput, sig) {
		return nil, ErrSignatureInvalid
	}
	return m, nil
}

// Artifact verifies that the bytes read from r match BOTH the SHA-256 and the
// BLAKE3 digest of a — both are required (AC-24). a MUST come from a
// signature-verified manifest (the expected hashes are read only from there). It
// streams r once with bounded memory; the caller is responsible for bounding r's
// length (the download ceiling is T25's concern).
func Artifact(a manifest.Artifact, r io.Reader) error {
	wantSHA, err := a.SHA256Digest()
	if err != nil {
		return fmt.Errorf("%w: sha256: %v", ErrManifestInvalid, err)
	}
	wantBLAKE3, err := a.BLAKE3Digest()
	if err != nil {
		return fmt.Errorf("%w: blake3: %v", ErrManifestInvalid, err)
	}
	shaHasher := sha256.New()
	blakeHasher := blake3.New()
	if _, err := io.Copy(io.MultiWriter(shaHasher, blakeHasher), r); err != nil {
		return fmt.Errorf("verify: reading artifact: %w", err)
	}
	gotSHA := shaHasher.Sum(nil)
	gotBLAKE3 := blakeHasher.Sum(nil)
	// Constant-time comparison on the raw digest bytes (consistency with the
	// hashing package's integrity primitive; the digests are public, so this is
	// defense-in-depth, not a secret-dependent path).
	if subtle.ConstantTimeCompare(gotSHA, wantSHA.Bytes()) != 1 {
		return fmt.Errorf("%w: sha256 got %s, want %s", ErrHashMismatch, hex.EncodeToString(gotSHA), wantSHA.HexDigest())
	}
	if subtle.ConstantTimeCompare(gotBLAKE3, wantBLAKE3.Bytes()) != 1 {
		return fmt.Errorf("%w: blake3 got %s, want %s", ErrHashMismatch, hex.EncodeToString(gotBLAKE3), wantBLAKE3.HexDigest())
	}
	return nil
}
