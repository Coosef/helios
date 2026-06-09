package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The published api/manifest.schema.json (Technical Design §390 frozen artifact)
// must match the embedded source of truth.
func TestPublishedSchemaMatchesEmbedded(t *testing.T) {
	published, err := os.ReadFile(filepath.Join("..", "..", "api", "manifest.schema.json"))
	if err != nil {
		t.Fatalf("reading api/manifest.schema.json: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(published), bytes.TrimSpace(schemaJSON)) {
		t.Error("api/manifest.schema.json is out of sync with the embedded pkg/manifest/manifest.schema.json")
	}
}

func TestEmbeddedSchemaCompiles(t *testing.T) {
	if _, err := compileSchema(schemaJSON); err != nil {
		t.Fatalf("embedded schema does not compile: %v", err)
	}
	if len(SchemaBytes()) == 0 {
		t.Error("SchemaBytes() returned empty")
	}
}

// The released_at pattern must gate the shape at the SCHEMA layer (symmetric with
// the Go RFC 3339 check), so the schema and Go agree like the other fields.
func TestSchemaRejectsMalformedReleasedAt(t *testing.T) {
	base := `{"schema_version":1,"target_version":"1.0.0","min_supported_version":"1.0.0","artifacts":[{"platform":"linux","arch":"amd64","url":"https://x/y","size_bytes":1,"sha256":"` +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `","blake3":"` +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `"}],"key_id":"k","key_revocation_list":[],"signature":"s"`
	for _, bad := range []string{`"not-a-date"`, `"hello"`, `"2026-06-10"`} {
		doc := base + `,"released_at":` + bad + `}`
		if err := validateSchema([]byte(doc)); err == nil {
			t.Errorf("schema accepted malformed released_at %s, want rejection", bad)
		}
	}
	for _, ok := range []string{`"2026-06-10T12:00:00Z"`, `"2026-06-10T12:00:00.5+02:00"`} {
		doc := base + `,"released_at":` + ok + `}`
		if err := validateSchema([]byte(doc)); err != nil {
			t.Errorf("schema rejected valid released_at %s: %v", ok, err)
		}
	}
}
