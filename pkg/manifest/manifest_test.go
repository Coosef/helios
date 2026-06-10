package manifest_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// validMap returns a fresh, valid manifest as a mutable map so tests can delete or
// corrupt individual fields.
func validMap() map[string]any {
	return map[string]any{
		"schema_version":        1,
		"target_version":        "1.2.0",
		"min_supported_version": "1.0.0",
		"artifacts": []any{
			map[string]any{
				"platform":   "windows",
				"arch":       "amd64",
				"url":        "https://dl.example.com/agent.exe",
				"size_bytes": 1048576,
				"sha256":     hex64,
				"blake3":     hex64,
			},
		},
		"key_id":              "test-key-1",
		"key_revocation_list": []any{},
		"signature":           "c2lnbmF0dXJl",
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestParseValid(t *testing.T) {
	m, err := manifest.Parse(mustJSON(t, validMap()))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.SchemaVersion != 1 || m.TargetVersion != "1.2.0" || m.MinSupportedVersion != "1.0.0" {
		t.Errorf("fields not parsed: %+v", m)
	}
	a, ok := m.ArtifactFor("windows", "amd64")
	if !ok {
		t.Fatal("ArtifactFor(windows,amd64) not found")
	}
	if a.SizeBytes != 1048576 {
		t.Errorf("size = %d, want 1048576", a.SizeBytes)
	}
	if _, ok := m.ArtifactFor("linux", "arm64"); ok {
		t.Error("ArtifactFor must return false for an absent target")
	}
}

func TestParseMissingRequiredFields(t *testing.T) {
	for _, field := range []string{
		"schema_version", "target_version", "min_supported_version",
		"artifacts", "key_id", "key_revocation_list", "signature",
	} {
		t.Run("missing_"+field, func(t *testing.T) {
			m := validMap()
			delete(m, field)
			if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrSchema) {
				t.Errorf("missing %q: err = %v, want ErrSchema", field, err)
			}
		})
	}
}

func TestParseMissingArtifactFields(t *testing.T) {
	for _, field := range []string{"platform", "arch", "url", "size_bytes", "sha256", "blake3"} {
		t.Run("missing_artifact_"+field, func(t *testing.T) {
			m := validMap()
			art := m["artifacts"].([]any)[0].(map[string]any)
			delete(art, field)
			if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrSchema) {
				t.Errorf("missing artifact.%q: err = %v, want ErrSchema", field, err)
			}
		})
	}
}

func TestParseHashValidation(t *testing.T) {
	cases := map[string]string{
		"too short":     strings.Repeat("0", 63),
		"too long":      strings.Repeat("0", 65),
		"non-hex":       strings.Repeat("z", 64),
		"uppercase":     strings.ToUpper(hex64),
		"empty":         "",
		"0x prefix":     "0x" + strings.Repeat("0", 62),
		"tagged sha256": "sha256:" + hex64,
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			m := validMap()
			m["artifacts"].([]any)[0].(map[string]any)["sha256"] = bad
			if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
				t.Errorf("sha256=%q accepted, want rejection", bad)
			}
		})
	}
}

func TestParsePlatformArchValidation(t *testing.T) {
	cases := []struct{ field, val string }{
		{"platform", "darwin"}, {"platform", "Windows"}, {"platform", ""},
		{"arch", "386"}, {"arch", "x86_64"}, {"arch", "ARM64"},
	}
	for _, c := range cases {
		t.Run(c.field+"_"+c.val, func(t *testing.T) {
			m := validMap()
			m["artifacts"].([]any)[0].(map[string]any)[c.field] = c.val
			if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrSchema) {
				t.Errorf("%s=%q: err = %v, want ErrSchema", c.field, c.val, err)
			}
		})
	}
}

func TestParseSizeValidation(t *testing.T) {
	for name, size := range map[string]int64{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			m := validMap()
			m["artifacts"].([]any)[0].(map[string]any)["size_bytes"] = size
			if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
				t.Errorf("size_bytes=%d accepted, want rejection", size)
			}
		})
	}
}

func TestParseURLMustBeHTTPS(t *testing.T) {
	for _, u := range []string{"http://x/y", "ftp://x/y", "/local/path", "x/y"} {
		m := validMap()
		m["artifacts"].([]any)[0].(map[string]any)["url"] = u
		if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrSchema) {
			t.Errorf("url=%q: err = %v, want ErrSchema", u, err)
		}
	}
}

func TestParseRollbackFloor(t *testing.T) {
	m := validMap()
	m["target_version"] = "1.0.0"
	m["min_supported_version"] = "2.0.0" // floor > target
	if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrVersionFloor) {
		t.Errorf("floor>target: err = %v, want ErrVersionFloor", err)
	}
}

func TestParseBadSemver(t *testing.T) {
	for _, v := range []string{"1.2", "1.2.3.4", "1.2.x", "v1.2.0", "01.0.0"} {
		m := validMap()
		m["target_version"] = v
		if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
			t.Errorf("target_version=%q accepted, want rejection", v)
		}
	}
}

func TestParseDuplicateArtifactTarget(t *testing.T) {
	m := validMap()
	art := m["artifacts"].([]any)[0]
	m["artifacts"] = []any{art, art} // same platform/arch twice
	if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrInvalidArtifact) {
		t.Errorf("duplicate target: err = %v, want ErrInvalidArtifact", err)
	}
}

func TestParseUnknownFieldsTolerated(t *testing.T) {
	m := validMap()
	m["future_top_level_field"] = map[string]any{"x": 1}
	m["artifacts"].([]any)[0].(map[string]any)["future_artifact_field"] = "ok"
	parsed, err := manifest.Parse(mustJSON(t, m))
	if err != nil {
		t.Fatalf("unknown fields must be tolerated (forward-compat), got: %v", err)
	}
	if parsed.TargetVersion != "1.2.0" {
		t.Errorf("known fields must still parse: %+v", parsed)
	}
}

func TestParseSchemaVersionMismatchRejected(t *testing.T) {
	m := validMap()
	m["schema_version"] = 2
	if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
		t.Error("schema_version=2 must be rejected")
	}
}

// Validate is also exercised directly (the Go semantic layer, independent of the
// schema) — these paths back-stop a future caller that builds a struct directly.
func TestValidateDirect(t *testing.T) {
	base := func() *manifest.Manifest {
		return &manifest.Manifest{
			SchemaVersion: 1, TargetVersion: "1.2.0", MinSupportedVersion: "1.0.0",
			Artifacts: []manifest.Artifact{{Platform: "linux", Arch: "arm64", URL: "https://x/y", SizeBytes: 10, SHA256: hex64, BLAKE3: hex64}},
			KeyID:     "k", KeyRevocationList: []string{}, Signature: "s",
		}
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("valid struct rejected: %v", err)
	}
	t.Run("bad schema_version", func(t *testing.T) {
		m := base()
		m.SchemaVersion = 2
		if err := m.Validate(); !errors.Is(err, manifest.ErrUnsupportedSchemaVersion) {
			t.Errorf("err = %v, want ErrUnsupportedSchemaVersion", err)
		}
	})
	t.Run("bad hash", func(t *testing.T) {
		m := base()
		m.Artifacts[0].SHA256 = "nothex"
		if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidArtifact) {
			t.Errorf("err = %v, want ErrInvalidArtifact", err)
		}
	})
	t.Run("zero size", func(t *testing.T) {
		m := base()
		m.Artifacts[0].SizeBytes = 0
		if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidArtifact) {
			t.Errorf("err = %v, want ErrInvalidArtifact", err)
		}
	})
	t.Run("no artifacts", func(t *testing.T) {
		m := base()
		m.Artifacts = nil
		if err := m.Validate(); !errors.Is(err, manifest.ErrInvalidArtifact) {
			t.Errorf("err = %v, want ErrInvalidArtifact", err)
		}
	})
}

func TestArtifactDigests(t *testing.T) {
	a := manifest.Artifact{SHA256: hex64, BLAKE3: hex64}
	sd, err := a.SHA256Digest()
	if err != nil {
		t.Fatalf("SHA256Digest: %v", err)
	}
	if sd.String() != "sha256:"+hex64 {
		t.Errorf("sha256 digest = %q", sd.String())
	}
	bd, err := a.BLAKE3Digest()
	if err != nil {
		t.Fatalf("BLAKE3Digest: %v", err)
	}
	if bd.String() != "blake3:"+hex64 {
		t.Errorf("blake3 digest = %q", bd.String())
	}
}

func TestParseNonObject(t *testing.T) {
	for _, bad := range []string{`[]`, `"x"`, `123`, `not json`, ``} {
		if _, err := manifest.Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%q) must error", bad)
		}
	}
}

func TestParseURLTightened(t *testing.T) {
	for _, u := range []string{
		"https://",                // empty host
		"https://x\nhttps://evil", // embedded newline (CRLF-injection style)
		"https://x y/z",           // embedded space
		"https:// ../x",           // space
		"https://\t/x",            // tab
	} {
		m := validMap()
		m["artifacts"].([]any)[0].(map[string]any)["url"] = u
		if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
			t.Errorf("url=%q accepted, want rejection", u)
		}
	}
	// a normal https URL with host + path still passes
	m := validMap()
	m["artifacts"].([]any)[0].(map[string]any)["url"] = "https://dl.example.com/v1/agent.exe"
	if _, err := manifest.Parse(mustJSON(t, m)); err != nil {
		t.Errorf("valid https URL rejected: %v", err)
	}
}

func TestParseSizeBytesUpperBound(t *testing.T) {
	m := validMap()
	m["artifacts"].([]any)[0].(map[string]any)["size_bytes"] = int64(1) << 53 // 2^53, above the exact bound
	if _, err := manifest.Parse(mustJSON(t, m)); err == nil {
		t.Error("size_bytes 2^53 accepted, want rejection (exact-signing bound)")
	}
	m["artifacts"].([]any)[0].(map[string]any)["size_bytes"] = int64(1)<<53 - 1 // 2^53-1, the max
	if _, err := manifest.Parse(mustJSON(t, m)); err != nil {
		t.Errorf("size_bytes 2^53-1 rejected: %v", err)
	}
}

func TestParseReleasedAt(t *testing.T) {
	// Wrong SHAPE is rejected at the schema layer (ErrSchema).
	m := validMap()
	m["released_at"] = "not-a-timestamp"
	if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrSchema) {
		t.Errorf("malformed released_at shape: err = %v, want ErrSchema", err)
	}
	// Shape-valid but semantically invalid (month 13) passes the schema pattern and
	// is caught by the Go RFC 3339 check (ErrInvalidReleasedAt).
	m["released_at"] = "2026-13-45T99:99:99Z"
	if _, err := manifest.Parse(mustJSON(t, m)); !errors.Is(err, manifest.ErrInvalidReleasedAt) {
		t.Errorf("invalid released_at value: err = %v, want ErrInvalidReleasedAt", err)
	}
	m["released_at"] = "2026-06-10T12:00:00Z"
	if _, err := manifest.Parse(mustJSON(t, m)); err != nil {
		t.Errorf("valid RFC3339 released_at rejected: %v", err)
	}
	delete(m, "released_at") // optional -> absent is fine
	if _, err := manifest.Parse(mustJSON(t, m)); err != nil {
		t.Errorf("absent released_at rejected: %v", err)
	}
}
