package mocksaas

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// indexEntry is one row of the committed test/fixtures/index.json.
type indexEntry struct {
	Name          string `json:"name"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	Baseline      string `json:"baseline"`
	ExpectProceed bool   `json:"expect_proceed"`
	ExpectReason  string `json:"expect_reason"`
	ManifestPath  string `json:"manifest"`
	ArtifactPath  string `json:"artifact"`
}

// WriteFixtures writes the deterministic fixture tree to dir: the public keyset, the
// signed manifests (raw signed bytes), the artifacts, and an index of expected
// outcomes. It writes ONLY public material (no private key — AC-35). cmd/mkfixtures
// and the drift test both call this, so a fresh write is byte-comparable to the
// committed tree.
func WriteFixtures(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ks, err := PublicKeySetJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "keyset.json"), append(ks, '\n'), 0o644); err != nil {
		return err
	}

	var index []indexEntry
	for _, f := range Generate() {
		caseDir := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(caseDir, 0o755); err != nil {
			return err
		}
		// RAW signed bytes (byte-identical to the in-memory fixture).
		if err := os.WriteFile(filepath.Join(caseDir, "manifest.json"), f.Manifest, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(caseDir, "artifact.bin"), f.Artifact, 0o644); err != nil {
			return err
		}
		index = append(index, indexEntry{
			Name: f.Name, Platform: f.Platform, Arch: f.Arch, Baseline: f.Baseline,
			ExpectProceed: f.ExpectProceed, ExpectReason: f.ExpectReason,
			ManifestPath: filepath.Join(f.Name, "manifest.json"),
			ArtifactPath: filepath.Join(f.Name, "artifact.bin"),
		})
	}
	idx, err := json.MarshalIndent(map[string]any{
		"schema_version": 1,
		"description":    "S1-T28 deterministic update fixtures. Regenerate with: go run ./cmd/mkfixtures. Verify against internal/mocksaas.TestKeySet() (NEVER trust.Embedded()).",
		"keyset":         "keyset.json",
		"fixtures":       index,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.json"), append(idx, '\n'), 0o644)
}
