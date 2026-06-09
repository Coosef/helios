// Package hashing provides algorithm-tagged content digests and verification for
// Beyz Backup. It is the shared integrity primitive reused by the backup engine,
// updater, manifest subsystem, and restore subsystem.
//
// Digests are ALWAYS stored in the algorithm-tagged format `<algo>:<hex>`, e.g.
//
//	blake3:9f4b2d...   (the default for content)
//	sha256:a81c9e...   (used by the update path)
//
// A raw, untagged hash is never produced or accepted: the tag makes the digest
// self-describing and lets verification dispatch on the algorithm, and lets new
// algorithms (e.g. sha512, blake2, xxhash) be added via Register without any
// change to the stored format. The package depends only on the standard library
// and a BLAKE3 implementation; it imports no other project packages, so it can
// be reused everywhere without introducing import cycles.
//
// Security: comparisons are constant-time, inputs are hashed via streaming
// (bounded memory — large files are never loaded whole), and digest parsing is
// strict (canonical lowercase hex, exact length, known algorithm).
package hashing

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/zeebo/blake3"
)

// Algorithm identifies a hash algorithm used as the prefix of a tagged digest.
type Algorithm string

// Supported algorithm identifiers. More may be added at runtime via Register.
const (
	BLAKE3 Algorithm = "blake3"
	SHA256 Algorithm = "sha256"
)

// DefaultAlgorithm is the algorithm used for content addressing (backup chunks,
// manifests). The update path uses SHA256 explicitly.
const DefaultAlgorithm = BLAKE3

// Sentinel errors. Callers should match with errors.Is.
var (
	ErrUnsupportedAlgorithm = errors.New("hashing: unsupported algorithm")
	ErrMalformedDigest      = errors.New("hashing: malformed digest")
	ErrHashMismatch         = errors.New("hashing: hash mismatch")
)

var (
	registryMu sync.RWMutex
	registry   = map[Algorithm]func() hash.Hash{}
)

// Register adds (or replaces) the hasher factory for algo. It is safe for
// concurrent use and is the extension point for future algorithms: registering
// one makes it usable everywhere without any change to the tagged digest format.
func Register(algo Algorithm, factory func() hash.Hash) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[algo] = factory
}

func init() {
	Register(BLAKE3, func() hash.Hash { return blake3.New() })
	Register(SHA256, sha256.New)
}

func newHasher(algo Algorithm) (hash.Hash, bool) {
	registryMu.RLock()
	factory, ok := registry[algo]
	registryMu.RUnlock()
	if !ok {
		return nil, false
	}
	return factory(), true
}

// IsSupportedAlgorithm reports whether algo has a registered hasher.
func IsSupportedAlgorithm(algo Algorithm) bool {
	registryMu.RLock()
	_, ok := registry[algo]
	registryMu.RUnlock()
	return ok
}

// SupportedAlgorithms returns the registered algorithms in sorted order.
func SupportedAlgorithms() []Algorithm {
	registryMu.RLock()
	out := make([]Algorithm, 0, len(registry))
	for a := range registry {
		out = append(out, a)
	}
	registryMu.RUnlock()
	slices.Sort(out)
	return out
}

// Digest is a parsed, validated algorithm-tagged digest.
type Digest struct {
	algo Algorithm
	hex  string
	raw  []byte
}

// Algorithm returns the digest's algorithm.
func (d Digest) Algorithm() Algorithm { return d.algo }

// HexDigest returns the lowercase hex encoding of the raw digest (without prefix).
func (d Digest) HexDigest() string { return d.hex }

// Bytes returns a copy of the raw digest bytes.
func (d Digest) Bytes() []byte { return append([]byte(nil), d.raw...) }

// String returns the tagged digest in `<algo>:<hex>` form.
func (d Digest) String() string { return string(d.algo) + ":" + d.hex }

// ParseDigest parses and validates a tagged digest string `<algo>:<hex>`. It
// rejects missing prefixes, empty hashes, non-lowercase or invalid hex, and
// wrong-length digests with ErrMalformedDigest, and unknown algorithms with
// ErrUnsupportedAlgorithm.
func ParseDigest(s string) (Digest, error) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return Digest{}, fmt.Errorf("%w: missing algorithm prefix in %q", ErrMalformedDigest, s)
	}
	algo := Algorithm(s[:i])
	hexPart := s[i+1:]
	if hexPart == "" {
		return Digest{}, fmt.Errorf("%w: empty hex digest in %q", ErrMalformedDigest, s)
	}
	h, ok := newHasher(algo)
	if !ok {
		return Digest{}, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, algo)
	}
	raw, err := hex.DecodeString(hexPart)
	if err != nil {
		return Digest{}, fmt.Errorf("%w: invalid hex in %q: %v", ErrMalformedDigest, s, err)
	}
	if hexPart != hex.EncodeToString(raw) {
		return Digest{}, fmt.Errorf("%w: digest hex must be lowercase in %q", ErrMalformedDigest, s)
	}
	if want := h.Size(); len(raw) != want {
		return Digest{}, fmt.Errorf("%w: %s digest must be %d bytes, got %d", ErrMalformedDigest, algo, want, len(raw))
	}
	return Digest{algo: algo, hex: hexPart, raw: raw}, nil
}

// HashReader streams r through the algo hasher and returns its tagged digest.
// It uses bounded memory (a streaming copy) and never loads the whole input.
func HashReader(algo Algorithm, r io.Reader) (Digest, error) {
	h, ok := newHasher(algo)
	if !ok {
		return Digest{}, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, algo)
	}
	if _, err := io.Copy(h, r); err != nil {
		return Digest{}, fmt.Errorf("hashing: reading input: %w", err)
	}
	sum := h.Sum(nil)
	return Digest{algo: algo, hex: hex.EncodeToString(sum), raw: sum}, nil
}

// HashBytes returns the tagged digest of data under algo.
func HashBytes(algo Algorithm, data []byte) (Digest, error) {
	return HashReader(algo, bytes.NewReader(data))
}

// HashFile streams the file at path through the algo hasher. The file is read
// incrementally, so arbitrarily large files use bounded memory.
func HashFile(algo Algorithm, path string) (Digest, error) {
	f, err := os.Open(path) // #nosec G304 -- hashing a caller-provided path is this function's purpose
	if err != nil {
		return Digest{}, fmt.Errorf("hashing: opening %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return HashReader(algo, f)
}

// VerifyReader recomputes the digest of r using the algorithm named in
// taggedDigest and compares it in constant time. It returns ErrHashMismatch on
// mismatch, or a parse error for an invalid taggedDigest.
func VerifyReader(taggedDigest string, r io.Reader) error {
	want, err := ParseDigest(taggedDigest)
	if err != nil {
		return err
	}
	got, err := HashReader(want.algo, r)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(want.raw, got.raw) != 1 {
		return fmt.Errorf("%w: expected %s, got %s", ErrHashMismatch, want.String(), got.String())
	}
	return nil
}

// VerifyBytes verifies data against taggedDigest.
func VerifyBytes(taggedDigest string, data []byte) error {
	return VerifyReader(taggedDigest, bytes.NewReader(data))
}

// VerifyFile verifies the file at path against taggedDigest, streaming the file.
func VerifyFile(taggedDigest string, path string) error {
	f, err := os.Open(path) // #nosec G304 -- verifying a caller-provided path is this function's purpose
	if err != nil {
		return fmt.Errorf("hashing: opening %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return VerifyReader(taggedDigest, f)
}
