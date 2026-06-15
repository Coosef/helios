package manifest_test

// Direct Manifest.Validate() coverage for the semantic checks that Parse's schema
// gate normally preempts. Validate is an INDEPENDENT authority (the production
// comment notes "reaching here means a struct built outside Parse", e.g. future
// signing tooling): it must reject a non-https/no-host artifact URL, a missing
// platform/arch, an over-bound size, a bad blake3 digest, a min>target floor, a
// malformed released_at, and a duplicate platform/arch target. These branches are
// not reachable through Parse (the schema rejects them first), so they are tested
// directly here. TestValidateDirect (manifest_test.go) covers schema_version, a
// bad sha256, a zero size, and the no-artifacts case; this file covers the rest.

import (
	"errors"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func TestValidateDirectArtifactAndFloorEdges(t *testing.T) {
	hex64 := strings.Repeat("a", 64)
	base := func() *manifest.Manifest {
		return &manifest.Manifest{
			SchemaVersion: 1, TargetVersion: "1.2.0", MinSupportedVersion: "1.0.0",
			Artifacts: []manifest.Artifact{{
				Platform: "linux", Arch: "arm64", URL: "https://x/y",
				SizeBytes: 10, SHA256: hex64, BLAKE3: hex64,
			}},
			KeyID: "k", KeyRevocationList: []string{}, Signature: "s",
		}
	}
	// Sanity: the base struct is valid so each case below isolates one defect.
	if err := base().Validate(); err != nil {
		t.Fatalf("base struct unexpectedly invalid: %v", err)
	}

	withArtifact := func(mut func(a *manifest.Artifact)) *manifest.Manifest {
		m := base()
		mut(&m.Artifacts[0])
		return m
	}

	// All of these must fail closed as ErrInvalidArtifact.
	artifactCases := map[string]*manifest.Manifest{
		"empty url":        withArtifact(func(a *manifest.Artifact) { a.URL = "" }),
		"non-https url":    withArtifact(func(a *manifest.Artifact) { a.URL = "http://x/y" }),
		"no-host url":      withArtifact(func(a *manifest.Artifact) { a.URL = "https:///y" }),
		"missing platform": withArtifact(func(a *manifest.Artifact) { a.Platform = "" }),
		"missing arch":     withArtifact(func(a *manifest.Artifact) { a.Arch = "" }),
		"oversize":         withArtifact(func(a *manifest.Artifact) { a.SizeBytes = 1 << 53 }), // > 2^53-1
		"bad blake3":       withArtifact(func(a *manifest.Artifact) { a.BLAKE3 = "nothex" }),
	}
	for name, m := range artifactCases {
		t.Run(name, func(t *testing.T) {
			if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidArtifact) {
				t.Errorf("err = %v, want ErrInvalidArtifact", err)
			}
		})
	}

	t.Run("duplicate platform/arch", func(t *testing.T) {
		m := base()
		m.Artifacts = append(m.Artifacts, m.Artifacts[0]) // same linux/arm64 twice
		if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidArtifact) {
			t.Errorf("err = %v, want ErrInvalidArtifact", err)
		}
	})

	t.Run("version floor min>target", func(t *testing.T) {
		m := base()
		m.MinSupportedVersion = "2.0.0" // exceeds target 1.2.0
		if err := m.Validate(); !errors.Is(err, manifest.ErrVersionFloor) {
			t.Errorf("err = %v, want ErrVersionFloor", err)
		}
	})

	t.Run("malformed released_at", func(t *testing.T) {
		m := base()
		m.ReleasedAt = "not-a-timestamp"
		if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidReleasedAt) {
			t.Errorf("err = %v, want ErrInvalidReleasedAt", err)
		}
	})
}
