package manifest_test

import (
	"errors"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func TestParseVersionValid(t *testing.T) {
	cases := map[string]manifest.Version{
		"0.0.0":       {0, 0, 0},
		"1.2.3":       {1, 2, 3},
		"10.20.30":    {10, 20, 30},
		"1.0.0":       {1, 0, 0},
		"0.1.0":       {0, 1, 0},
		"100.200.300": {100, 200, 300},
	}
	for s, want := range cases {
		got, err := manifest.ParseVersion(s)
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("ParseVersion(%q) = %+v, want %+v", s, got, want)
		}
		if got.String() != s {
			t.Errorf("String() = %q, want %q", got.String(), s)
		}
	}
}

func TestParseVersionInvalid(t *testing.T) {
	for _, s := range []string{
		"", "1", "1.2", "1.2.3.4", "1.2.x", "v1.2.0", "01.0.0", "1.02.0",
		"1.0.0-rc1", "1.0.0+build", " 1.0.0", "1.0.0 ", "-1.0.0", "+1.0.0",
		"1..0", "1.0.", ".1.0", "0x1.0.0",
	} {
		if _, err := manifest.ParseVersion(s); !errors.Is(err, manifest.ErrInvalidVersion) {
			t.Errorf("ParseVersion(%q) err = %v, want ErrInvalidVersion", s, err)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexical
		{"1.0.9", "1.0.10", -1},
		{"1.1.0", "1.0.9", 1},
		{"0.0.1", "0.0.0", 1},
		{"10.0.0", "9.9.9", 1},
	}
	for _, c := range cases {
		va, _ := manifest.ParseVersion(c.a)
		vb, _ := manifest.ParseVersion(c.b)
		if got := va.Compare(vb); got != c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
		// antisymmetry
		if got := vb.Compare(va); got != -c.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestParseVersionSegmentLengthBound(t *testing.T) {
	// 9-digit segment OK (platform-independent); 10-digit rejected by BOTH layers.
	if _, err := manifest.ParseVersion("999999999.0.0"); err != nil {
		t.Errorf("9-digit segment rejected: %v", err)
	}
	if _, err := manifest.ParseVersion("1000000000.0.0"); !errors.Is(err, manifest.ErrInvalidVersion) {
		t.Errorf("10-digit segment: err = %v, want ErrInvalidVersion", err)
	}
}
