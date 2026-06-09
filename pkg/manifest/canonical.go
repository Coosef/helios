package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gowebpki/jcs"
)

// ErrCanonicalize is returned when canonicalization fails (not a JSON object,
// duplicate keys, or trailing data).
var ErrCanonicalize = errors.New("manifest: canonicalization failed")

// CanonicalSigningInput returns the RFC 8785 (JCS) canonical bytes over which the
// Ed25519 signature is computed and verified: the manifest with the `signature`
// field removed.
//
// It operates on the RAW JSON, not the typed struct, so that unknown fields are
// PRESERVED — a forward manifest carrying additional (signed) fields canonicalizes
// identically for the signer and the verifier (S1-T23), and the signature stays
// valid across agent versions. This is the single signing-input definition shared
// by the signer and the verifier; getting it wrong would break every signature.
//
// Duplicate object keys are REJECTED at every level (top-level here, nested via
// jcs.Transform) per RFC 8785 / I-JSON, so there is exactly one canonical signing
// input per logical manifest and this verifier agrees with any compliant signer.
//
// NUMBER PRECISION: RFC 8785 canonicalizes JSON numbers as IEEE-754 float64, so an
// integer manifest field is only signed EXACTLY when it is <= 2^53-1. The one
// security-relevant integer, artifact.size_bytes, is schema-bounded to 2^53-1 to
// guarantee its signed form is exact; new integer fields must respect the same bound.
func CanonicalSigningInput(raw []byte) ([]byte, error) {
	obj, err := splitTopLevelObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanonicalize, err)
	}
	delete(obj, SignatureField)
	// Re-marshal the remaining fields; JCS then sorts keys and normalizes numbers/
	// strings deterministically per RFC 8785, so key order here is irrelevant. JCS
	// also rejects duplicate keys inside nested objects.
	reassembled, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanonicalize, err)
	}
	canon, err := jcs.Transform(reassembled)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanonicalize, err)
	}
	return canon, nil
}

// splitTopLevelObject decodes a JSON object into its top-level fields, REJECTING
// duplicate top-level keys (which json.Unmarshal into a map would silently collapse
// last-wins, allowing >1 byte-document per signing input) and trailing data.
func splitTopLevelObject(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("manifest must be a JSON object")
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
	// Reject trailing data after the top-level object (match json.Unmarshal strictness).
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("unexpected trailing data after manifest object")
	}
	return out, nil
}
