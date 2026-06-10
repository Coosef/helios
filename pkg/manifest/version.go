package manifest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidVersion is returned for a string that is not a clean MAJOR.MINOR.PATCH
// release version. Manifest versions are releases only (no pre-release/build
// metadata) so the anti-rollback comparison (UPD-6) is total and unambiguous.
var ErrInvalidVersion = errors.New("manifest: invalid version (want MAJOR.MINOR.PATCH)")

// Version is a parsed semantic release version. Pre-release/build metadata is
// intentionally unsupported for manifests (see ErrInvalidVersion).
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion parses a strict MAJOR.MINOR.PATCH string: three non-negative
// integers, no leading zeros (except "0"), no sign, no extra segments.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, s)
	}
	var v Version
	for i, p := range parts {
		n, err := parseSegment(p)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %q", ErrInvalidVersion, s)
		}
		switch i {
		case 0:
			v.Major = n
		case 1:
			v.Minor = n
		case 2:
			v.Patch = n
		}
	}
	return v, nil
}

// maxSegmentDigits bounds a version segment to <= 9 digits (< 10^9 < 2^31) so the
// parse is platform-independent (never overflows int on 32-bit) and agrees with
// the schema pattern, which uses the same bound.
const maxSegmentDigits = 9

// parseSegment parses one version segment: a non-empty run of <= 9 ASCII digits
// with no leading zero (except the single "0").
func parseSegment(p string) (int, error) {
	if p == "" {
		return 0, errors.New("empty segment")
	}
	if len(p) > maxSegmentDigits {
		return 0, fmt.Errorf("segment too long %q", p)
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", p)
		}
	}
	if len(p) > 1 && p[0] == '0' {
		return 0, fmt.Errorf("leading zero %q", p)
	}
	return strconv.Atoi(p)
}

// Compare returns -1 if v < o, 0 if equal, +1 if v > o.
func (v Version) Compare(o Version) int {
	if c := cmpInt(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmpInt(v.Minor, o.Minor); c != 0 {
		return c
	}
	return cmpInt(v.Patch, o.Patch)
}

// String renders the version as MAJOR.MINOR.PATCH.
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
