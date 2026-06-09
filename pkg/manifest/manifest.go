// Package manifest defines the SIGNED update-manifest contract (S1-T21, ADR-002):
// the Go structs, the JSON Schema, RFC 8785 (JCS) canonical signing input, and
// structural validation shared by the updater (T22-T27) and future signing tooling.
//
// SCOPE: this package defines and validates the contract ONLY. It does NOT verify
// the Ed25519 signature, fetch a manifest, download a binary, or apply an update —
// those are S1-T22/T23/T24/T25. The `signature`, `key_id`, and `key_revocation_list`
// fields are carried and structurally validated here; their TRUST evaluation is T23.
//
// Forward compatibility: unknown fields are tolerated (Technical Design §383). A
// manifest's integrity is guaranteed by the signature over the full JCS canonical
// form (which includes any unknown fields), not by rejecting unknown fields. A
// breaking change is signalled by bumping schema_version, which IS rejected.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/beyzbackup/beyz-backup/pkg/hashing"
)

// SchemaVersion is the only update-manifest schema version this build understands.
const SchemaVersion = 1

// SignatureField is the manifest field excluded from the canonical signing input.
const SignatureField = "signature"

// maxExactInt is the largest integer with an exact IEEE-754 float64 (hence exact
// RFC 8785 canonical) representation: 2^53-1. size_bytes is bounded to it so the
// signed canonical form always matches the int64 the agent acts on.
const maxExactInt = 1<<53 - 1

var (
	// ErrSchema is returned when a manifest fails JSON-Schema validation.
	ErrSchema = errors.New("manifest: schema validation failed")
	// ErrUnsupportedSchemaVersion is returned for a schema_version this build can't parse.
	ErrUnsupportedSchemaVersion = errors.New("manifest: unsupported schema_version")
	// ErrVersionFloor is returned when min_supported_version exceeds target_version.
	ErrVersionFloor = errors.New("manifest: min_supported_version exceeds target_version")
	// ErrInvalidArtifact is returned for a structurally invalid artifact entry.
	ErrInvalidArtifact = errors.New("manifest: invalid artifact")
	// ErrInvalidHash is returned when an artifact hash is not a valid tagged digest.
	ErrInvalidHash = errors.New("manifest: invalid artifact hash")
	// ErrInvalidReleasedAt is returned when released_at is present but not RFC 3339.
	ErrInvalidReleasedAt = errors.New("manifest: invalid released_at (want RFC 3339)")
)

// Manifest is a signed update manifest (ADR-002).
type Manifest struct {
	SchemaVersion       int        `json:"schema_version"`
	TargetVersion       string     `json:"target_version"`
	MinSupportedVersion string     `json:"min_supported_version"`
	AllowDowngrade      bool       `json:"allow_downgrade,omitempty"`
	ReleasedAt          string     `json:"released_at,omitempty"`
	RolloutCohortPct    *int       `json:"rollout_cohort_pct,omitempty"`
	UpdateAllowed       *bool      `json:"update_allowed,omitempty"`
	Artifacts           []Artifact `json:"artifacts"`
	KeyID               string     `json:"key_id"`
	KeyRevocationList   []string   `json:"key_revocation_list"`
	// Signature is base64 Ed25519 over the canonical signing input (this manifest
	// WITHOUT the signature field). Carried here; VERIFIED in S1-T23.
	Signature string `json:"signature"`
}

// Artifact is one platform/arch binary's download metadata. The sha256/blake3
// fields carry lowercase hex (the field name is the algorithm tag).
type Artifact struct {
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	BLAKE3    string `json:"blake3"`
}

// SHA256Digest returns the artifact's SHA-256 as a validated tagged Digest.
func (a Artifact) SHA256Digest() (hashing.Digest, error) {
	return hashing.ParseDigest(string(hashing.SHA256) + ":" + a.SHA256)
}

// BLAKE3Digest returns the artifact's BLAKE3 as a validated tagged Digest.
func (a Artifact) BLAKE3Digest() (hashing.Digest, error) {
	return hashing.ParseDigest(string(hashing.BLAKE3) + ":" + a.BLAKE3)
}

// Parse validates data against the schema, decodes it (ignoring unknown fields for
// forward compatibility), and runs semantic validation. It does NOT verify the
// signature (S1-T23).
func Parse(data []byte) (*Manifest, error) {
	if err := validateSchema(data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSchema, err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	// NOTE: DisallowUnknownFields is intentionally NOT set — unknown fields are
	// tolerated for forward compatibility (Technical Design §383); the signature
	// covers them via the canonical form.
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSchema, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate runs cross-field and digest semantic checks not expressible (or worth
// duplicating) in the schema: the supported schema_version, the anti-rollback
// floor (min <= target), per-artifact tagged-digest validity, and unique
// platform/arch targeting. Structural/type/enum/pattern checks are the schema's.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: %d (want %d)", ErrUnsupportedSchemaVersion, m.SchemaVersion, SchemaVersion)
	}
	target, err := ParseVersion(m.TargetVersion)
	if err != nil {
		return fmt.Errorf("target_version: %w", err)
	}
	floor, err := ParseVersion(m.MinSupportedVersion)
	if err != nil {
		return fmt.Errorf("min_supported_version: %w", err)
	}
	if floor.Compare(target) > 0 {
		return fmt.Errorf("%w: %s > %s", ErrVersionFloor, floor, target)
	}
	if m.ReleasedAt != "" {
		if _, err := time.Parse(time.RFC3339, m.ReleasedAt); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidReleasedAt, err)
		}
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("%w: no artifacts", ErrInvalidArtifact)
	}
	seen := make(map[string]struct{}, len(m.Artifacts))
	for i, a := range m.Artifacts {
		if err := a.validate(); err != nil {
			return fmt.Errorf("%w[%d]: %v", ErrInvalidArtifact, i, err)
		}
		key := a.Platform + "/" + a.Arch
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%w: duplicate platform/arch %q", ErrInvalidArtifact, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (a Artifact) validate() error {
	if a.Platform == "" || a.Arch == "" {
		return errors.New("missing platform or arch")
	}
	if err := validateHTTPSURL(a.URL); err != nil {
		return err
	}
	if a.SizeBytes <= 0 {
		return fmt.Errorf("size_bytes must be positive, got %d", a.SizeBytes)
	}
	if a.SizeBytes > maxExactInt {
		// Above 2^53 the JCS canonical (float64) form would not be exact; the schema
		// also bounds this, so reaching here means a struct built outside Parse.
		return fmt.Errorf("size_bytes %d exceeds the exact-signing bound %d", a.SizeBytes, maxExactInt)
	}
	if _, err := a.SHA256Digest(); err != nil {
		return fmt.Errorf("%w: sha256: %v", ErrInvalidHash, err)
	}
	if _, err := a.BLAKE3Digest(); err != nil {
		return fmt.Errorf("%w: blake3: %v", ErrInvalidHash, err)
	}
	return nil
}

// validateHTTPSURL requires a well-formed absolute HTTPS URL with a host (the
// schema pattern is a coarse first gate; this is the authoritative check). It
// rejects empty authority, non-https schemes, and embedded control characters
// (url.Parse rejects ASCII control bytes, closing CRLF-injection vectors).
func validateHTTPSURL(raw string) error {
	if raw == "" {
		return errors.New("missing url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url must be https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("url %q has no host", raw)
	}
	return nil
}

// ArtifactFor returns the artifact for the given platform/arch, or false if none.
func (m *Manifest) ArtifactFor(platform, arch string) (Artifact, bool) {
	for _, a := range m.Artifacts {
		if a.Platform == platform && a.Arch == arch {
			return a, true
		}
	}
	return Artifact{}, false
}
