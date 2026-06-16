// Package license loads and verifies the server-signed, offline-verifiable license
// blob (S1-T17, LIC-1/4/5).
//
// Verification is REAL and FAIL-CLOSED: a detached Ed25519 signature over the
// RFC 8785 (JCS) canonical claims (the token minus its signature field), against a
// SEPARATE embedded license public-key set (never the update trust anchor). There is
// no return-true shortcut. The signature is the only anti-tamper mechanism for
// agent-side license state (LIC-5): an operator who edits a claim breaks the signature.
//
// The CONSEQUENCES are ADVISORY in Sprint 1 (LIC-4): a missing, malformed, expired,
// not-yet-valid, tenant-mismatched, or signature-invalid license is recorded but
// NEVER blocks startup, enrollment, heartbeat, or any agent operation. Expiry and
// tenant binding are PARSED and classified but NOT enforced; enforcement (seats,
// quota, expiry, revocation) lands server-side in a later sprint.
package license

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gowebpki/jcs"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
)

// SchemaVersion is the only license-claim schema version this build understands.
const SchemaVersion = 1

// signatureField is the claim excluded from the canonical signing input.
const signatureField = "signature"

// Typed errors. Match with errors.Is.
var (
	// ErrNoTrustAnchor is returned when no license key set is supplied (fail closed).
	ErrNoTrustAnchor = errors.New("license: no trust anchor (nil key set)")
	// ErrMalformed is returned for a structurally invalid token or envelope.
	ErrMalformed = errors.New("license: malformed license token")
	// ErrUnsupportedSchemaVersion is returned for a schema_version this build can't verify.
	ErrUnsupportedSchemaVersion = errors.New("license: unsupported schema_version")
	// ErrMalformedSignature is returned when the signature is not base64 or not the
	// Ed25519 signature size.
	ErrMalformedSignature = errors.New("license: malformed signature")
	// ErrSignatureInvalid is returned when the Ed25519 signature does not verify.
	ErrSignatureInvalid = errors.New("license: signature verification failed")
	// ErrNoLicense is returned when no license blob is present (absent/empty). It is
	// the tolerated, advisory "no license" state, not a failure.
	ErrNoLicense = errors.New("license: no license blob present")
)

// Claims is the frozen license claim set (S1-T17). It is signed by the license
// authority; the agent verifies it offline. Unknown (signed) fields are tolerated
// for forward compatibility — they are still covered by the signature via the
// canonical form, exactly like the update manifest.
type Claims struct {
	SchemaVersion int    `json:"schema_version"`
	LicenseID     string `json:"license_id"`
	TenantID      string `json:"tenant_id"`
	ParentOrgID   string `json:"parent_org_id"`
	Plan          string `json:"plan"`
	Seats         int    `json:"seats"`
	QuotaBytes    int64  `json:"quota_bytes"`
	IssuedAt      string `json:"issued_at"`  // RFC 3339
	NotBefore     string `json:"not_before"` // RFC 3339
	NotAfter      string `json:"not_after"`  // RFC 3339
	KeyID         string `json:"key_id"`
	// Signature is the base64 Ed25519 signature over the JCS canonical form of the
	// token WITHOUT this field. Carried in the token; verified by Verify.
	Signature string `json:"signature"`
}

// Verify parses tokenJSON and verifies its detached Ed25519 signature over the JCS
// canonical signing input (the token minus the signature field), using the key
// selected by the token's key_id from keys. On success it returns the verified Claims.
//
// Order of rejection (fail closed, mirroring the update verifier): nil key set ->
// structural/canonical invalidity -> unsupported schema_version -> malformed
// signature -> unknown/revoked key_id -> invalid signature.
func Verify(tokenJSON []byte, keys *trust.KeySet) (*Claims, error) {
	if keys == nil {
		return nil, ErrNoTrustAnchor
	}
	var c Claims
	if err := json.Unmarshal(tokenJSON, &c); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if c.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%w: %d (want %d)", ErrUnsupportedSchemaVersion, c.SchemaVersion, SchemaVersion)
	}
	signingInput, err := CanonicalSigningInput(tokenJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64: %v", ErrMalformedSignature, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: %d bytes, want %d", ErrMalformedSignature, len(sig), ed25519.SignatureSize)
	}
	// Select the key by the token's OWN key_id (authoritative — it is inside the
	// signed payload). Propagates trust.ErrUnknownKey / trust.ErrRevokedKey.
	pub, err := keys.Key(c.KeyID)
	if err != nil {
		return nil, fmt.Errorf("license: untrusted key_id: %w", err)
	}
	if !ed25519.Verify(pub, signingInput, sig) {
		return nil, ErrSignatureInvalid
	}
	return &c, nil
}

// CanonicalSigningInput returns the RFC 8785 (JCS) canonical bytes of the token with
// the `signature` field removed — the bytes the signature commits to. It operates on
// the RAW JSON (not the typed struct) so unknown signed fields are preserved, and it
// rejects duplicate top-level keys / trailing data (a key-confusion / multi-document
// vector). This is the single signing-input definition shared by the verifier and by
// any license signer (the SaaS issuer or an offline signing tool), exactly mirroring
// manifest.CanonicalSigningInput.
func CanonicalSigningInput(raw []byte) ([]byte, error) {
	obj, err := splitTopLevelObject(raw)
	if err != nil {
		return nil, err
	}
	delete(obj, signatureField)
	reassembled, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(reassembled) // sorts keys + normalizes per RFC 8785; rejects nested dup keys
}

// splitTopLevelObject decodes a JSON object into its top-level fields, rejecting
// duplicate top-level keys (which a plain map would silently collapse last-wins,
// admitting >1 document per signing input) and trailing data.
func splitTopLevelObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("license token must be a JSON object")
	}
	out := make(map[string]json.RawMessage)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("expected string object key")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate top-level key %q", key)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		out[key] = val
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("unexpected trailing data after license token")
	}
	return out, nil
}

// envelope is the persisted license-blob wrapper (proto.LicenseBlob, JSON-encoded by
// the enrollment use-case into the state store). The actual signed token is the
// base64 `value`.
type envelope struct {
	Encoding     string `json:"encoding"`
	SignatureAlg string `json:"signature_alg"`
	KeyID        string `json:"key_id"`
	Value        string `json:"value"`
}

// DecodeToken extracts the raw signed token JSON from the persisted license-blob
// envelope (base64-decoding envelope.value). It returns ErrNoLicense when the blob
// is absent/empty, or ErrMalformed for a structurally invalid envelope or an
// unsupported encoding/signature_alg.
func DecodeToken(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, ErrNoLicense
	}
	var env envelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return nil, fmt.Errorf("%w: envelope: %v", ErrMalformed, err)
	}
	if env.Value == "" {
		return nil, ErrNoLicense
	}
	if env.Encoding != "" && env.Encoding != "base64" {
		return nil, fmt.Errorf("%w: unsupported encoding %q", ErrMalformed, env.Encoding)
	}
	if env.SignatureAlg != "" && env.SignatureAlg != "ed25519" {
		return nil, fmt.Errorf("%w: unsupported signature_alg %q", ErrMalformed, env.SignatureAlg)
	}
	token, err := base64.StdEncoding.DecodeString(env.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: value not base64: %v", ErrMalformed, err)
	}
	return token, nil
}

// Status is the advisory classification of the agent's license at startup.
type Status string

const (
	// StatusValid means the signature verified and the license is within its validity
	// window and matches the agent's tenant.
	StatusValid Status = "valid"
	// StatusMissing means no license blob is present (the tolerated Sprint-1 default).
	StatusMissing Status = "missing"
	// StatusSignatureInvalid means the blob could not be verified (malformed, unknown/
	// revoked key, or a failed signature) — the anti-tamper signal (LIC-5).
	StatusSignatureInvalid Status = "signature_invalid"
	// StatusNotYetValid means a validly-signed license whose not_before is in the future.
	StatusNotYetValid Status = "not_yet_valid"
	// StatusExpired means a validly-signed license past its not_after (parsed, not enforced).
	StatusExpired Status = "expired"
	// StatusTenantMismatch means a validly-signed license bound to a different tenant
	// than the enrolled agent (parsed, not enforced).
	StatusTenantMismatch Status = "tenant_mismatch"
)

// Result is the advisory outcome of a license load+verify at startup.
type Result struct {
	Status Status
	// Claims is non-nil whenever the signature verified (even if expired/mismatched).
	Claims *Claims
	// Err is the underlying error for a non-valid, non-missing status.
	Err error
}

// Evaluate verifies token against keys and classifies it ADVISORILY against now and
// the agent's tenant. The signature is verified fail-closed; expiry and tenant
// binding are parsed and classified but NOT enforced (Sprint 1). A non-Valid result
// is never a reason to block the agent. now is injected (no wall-clock dependency in
// this package).
func Evaluate(token []byte, keys *trust.KeySet, now time.Time, agentTenantID string) Result {
	claims, err := Verify(token, keys)
	if err != nil {
		return Result{Status: StatusSignatureInvalid, Err: err}
	}
	// Signature verified — claims are trusted. The checks below are advisory only.
	if nb := parseTime(claims.NotBefore); !nb.IsZero() && now.Before(nb) {
		return Result{Status: StatusNotYetValid, Claims: claims}
	}
	if na := parseTime(claims.NotAfter); !na.IsZero() && now.After(na) {
		return Result{Status: StatusExpired, Claims: claims}
	}
	if agentTenantID != "" && claims.TenantID != "" && claims.TenantID != agentTenantID {
		return Result{Status: StatusTenantMismatch, Claims: claims}
	}
	return Result{Status: StatusValid, Claims: claims}
}

// parseTime parses an RFC 3339 timestamp, returning the zero time for an empty or
// malformed value (treated as "not set" — advisory, never enforced).
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
