package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shipped configs/config.schema.json must match the embedded source of
// truth. If this fails, run `task gen:config`.
func TestConfigsSchemaCopyMatchesEmbedded(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", "..", "configs", "config.schema.json"))
	if err != nil {
		t.Fatalf("reading configs/config.schema.json: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(shipped), bytes.TrimSpace(schemaJSON)) {
		t.Error("configs/config.schema.json is out of date with the embedded schema; run `task gen:config`")
	}
}

func TestDefaultLogPathForBothPlatforms(t *testing.T) {
	if got := defaultLogPathFor("windows"); !strings.Contains(got, "ProgramData") {
		t.Errorf("windows path = %q", got)
	}
	if got := defaultLogPathFor("linux"); !strings.HasPrefix(got, "/var/log/") {
		t.Errorf("linux path = %q", got)
	}
}

func TestCompileSchema(t *testing.T) {
	if _, err := compileSchema([]byte("{not valid json")); err == nil {
		t.Error("compileSchema(invalid JSON) should error")
	}
	if _, err := compileSchema([]byte(`{"$ref":"#/$defs/missing"}`)); err == nil {
		t.Error("compileSchema(unresolvable $ref) should error")
	}
	if _, err := compileSchema(schemaJSON); err != nil {
		t.Errorf("compileSchema(embedded) error: %v", err)
	}
}

func TestValidateAgainstSchemaRejectsBadInstanceJSON(t *testing.T) {
	if err := validateAgainstSchema([]byte("{not valid json")); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("validateAgainstSchema(bad json) = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeConfigRejectsUnrepresentableInteger(t *testing.T) {
	// Valid per the schema (an integer >= 1) but too large for a Go int.
	if _, err := decodeConfig([]byte(`{"config_version": 999999999999999999999}`)); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("decodeConfig(huge int) = %v, want ErrInvalidConfig", err)
	}
	if _, err := decodeConfig([]byte(`{"config_version": 1}`)); err != nil {
		t.Errorf("decodeConfig(valid) error: %v", err)
	}
}

func TestEnvKeyMapCoversAllLeafKeys(t *testing.T) {
	em := envKeyMap()
	if len(em) != len(configKeyPaths) {
		t.Fatalf("envKeyMap size = %d, want %d", len(em), len(configKeyPaths))
	}
	if em["GENERAL_API_BASE_URL"] != "general.api_base_url" {
		t.Errorf("mapping wrong: %q", em["GENERAL_API_BASE_URL"])
	}
}
