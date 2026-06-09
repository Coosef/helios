package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/config"
)

const validYAML = `
config_version: 1
general:
  api_base_url: "https://api.example.com"
heartbeat:
  heartbeat_interval_seconds: 30
  task_poll_interval_seconds: 120
logging:
  level: "debug"
  format: "console"
  file_path: "/tmp/agent.log"
storage:
  default_storage_backend: "s3"
  storage_timeout_seconds: 30
updater:
  update_channel: "beta"
  update_check_interval_seconds: 600
`

func discard() *config.BootstrapLogger { return config.NewBootstrapLoggerTo(io.Discard) }

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidConfig(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, validYAML), discard())
	if err != nil {
		t.Fatalf("Load valid config: %v", err)
	}
	if cfg.ConfigVersion != 1 {
		t.Errorf("ConfigVersion = %d, want 1", cfg.ConfigVersion)
	}
	if cfg.General.APIBaseURL != "https://api.example.com" {
		t.Errorf("APIBaseURL = %q", cfg.General.APIBaseURL)
	}
	if cfg.Heartbeat.HeartbeatIntervalSeconds != 30 || cfg.Heartbeat.TaskPollIntervalSeconds != 120 {
		t.Errorf("heartbeat = %+v", cfg.Heartbeat)
	}
	if cfg.Logging.Level != "debug" || cfg.Logging.Format != "console" || cfg.Logging.FilePath != "/tmp/agent.log" {
		t.Errorf("logging = %+v", cfg.Logging)
	}
	if cfg.Storage.DefaultStorageBackend != "s3" || cfg.Storage.StorageTimeoutSeconds != 30 {
		t.Errorf("storage = %+v", cfg.Storage)
	}
	if cfg.Updater.UpdateChannel != "beta" || cfg.Updater.UpdateCheckIntervalSeconds != 600 {
		t.Errorf("updater = %+v", cfg.Updater)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Only the required field is set; everything else must come from defaults.
	cfg, err := config.Load(writeConfig(t, "general:\n  api_base_url: \"https://api.example.com\"\n"), discard())
	if err != nil {
		t.Fatalf("Load minimal config: %v", err)
	}
	if cfg.ConfigVersion != config.CurrentConfigVersion {
		t.Errorf("ConfigVersion default = %d, want %d", cfg.ConfigVersion, config.CurrentConfigVersion)
	}
	def := config.DefaultConfig()
	if cfg.Heartbeat != def.Heartbeat || cfg.Logging != def.Logging ||
		cfg.Storage != def.Storage || cfg.Updater != def.Updater {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadMissingRequiredField(t *testing.T) {
	// general present but api_base_url omitted.
	_, err := config.Load(writeConfig(t, "general:\n  region: \"eu\"\n"), discard())
	if !errors.Is(err, config.ErrSchemaValidation) {
		t.Fatalf("error = %v, want ErrSchemaValidation", err)
	}
}

func TestLoadUnknownFieldRejected(t *testing.T) {
	const base = "general:\n  api_base_url: \"https://a.example.com\"\n"
	cases := map[string]string{
		"top-level":  base + "extra_root: 1\n",
		"nested":     "general:\n  api_base_url: \"https://a.example.com\"\n  surprise: true\n",
		"enroll-tok": "general:\n  api_base_url: \"https://a.example.com\"\n  enrollment_token: \"bzt_should_be_rejected\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(writeConfig(t, body), discard()); !errors.Is(err, config.ErrSchemaValidation) {
				t.Errorf("error = %v, want ErrSchemaValidation", err)
			}
		})
	}
}

func TestLoadSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"bad enum level":   "general:\n  api_base_url: \"https://a.example.com\"\nlogging:\n  level: \"verbose\"\n",
		"bad enum channel": "general:\n  api_base_url: \"https://a.example.com\"\nupdater:\n  update_channel: \"nightly\"\n",
		"interval too low": "general:\n  api_base_url: \"https://a.example.com\"\nheartbeat:\n  heartbeat_interval_seconds: 5\n",
		"interval too high": "general:\n  api_base_url: \"https://a.example.com\"\nheartbeat:\n  task_poll_interval_seconds: 999999\n",
		"non-https url":    "general:\n  api_base_url: \"http://insecure.example.com\"\n",
		"wrong type":       "general:\n  api_base_url: \"https://a.example.com\"\nheartbeat:\n  heartbeat_interval_seconds: \"sixty\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(writeConfig(t, body), discard()); !errors.Is(err, config.ErrSchemaValidation) {
				t.Errorf("error = %v, want ErrSchemaValidation", err)
			}
		})
	}
}

func TestLoadConfigVersionTooNew(t *testing.T) {
	body := "config_version: 99\ngeneral:\n  api_base_url: \"https://a.example.com\"\n"
	_, err := config.Load(writeConfig(t, body), discard())
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestLoadDecodeErrorOnHugeConfigVersion(t *testing.T) {
	// config_version is schema-valid (integer >= 1) but too large for a Go int,
	// so it passes schema validation and fails at decode time.
	body := "config_version: 999999999999999999999\ngeneral:\n  api_base_url: \"https://a.example.com\"\n"
	if _, err := config.Load(writeConfig(t, body), discard()); !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestEnvOverrides(t *testing.T) {
	p := writeConfig(t, validYAML)
	t.Setenv("BEYZ_LOGGING_LEVEL", "error")
	t.Setenv("BEYZ_HEARTBEAT_HEARTBEAT_INTERVAL_SECONDS", "90")
	t.Setenv("BEYZ_GENERAL_API_BASE_URL", "https://override.example.com")
	t.Setenv("BEYZ_NONSENSE_UNKNOWN", "ignored") // unknown env var must be ignored

	cfg, err := config.Load(p, discard())
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("env override level = %q, want error", cfg.Logging.Level)
	}
	if cfg.Heartbeat.HeartbeatIntervalSeconds != 90 {
		t.Errorf("env override interval = %d, want 90", cfg.Heartbeat.HeartbeatIntervalSeconds)
	}
	if cfg.General.APIBaseURL != "https://override.example.com" {
		t.Errorf("env override url = %q", cfg.General.APIBaseURL)
	}
}

func TestLoadNoFileEnvOnly(t *testing.T) {
	// Empty path: configuration comes from defaults + environment only.
	t.Setenv("BEYZ_GENERAL_API_BASE_URL", "https://envonly.example.com")
	cfg, err := config.Load("", discard())
	if err != nil {
		t.Fatalf("Load(no file): %v", err)
	}
	if cfg.General.APIBaseURL != "https://envonly.example.com" {
		t.Errorf("api_base_url = %q", cfg.General.APIBaseURL)
	}
	if cfg.Heartbeat.HeartbeatIntervalSeconds != 60 {
		t.Errorf("defaults not applied without a file: %+v", cfg.Heartbeat)
	}
}

func TestEnvIntegerParseError(t *testing.T) {
	t.Setenv("BEYZ_HEARTBEAT_HEARTBEAT_INTERVAL_SECONDS", "not-a-number")
	if _, err := config.Load(writeConfig(t, validYAML), discard()); !errors.Is(err, config.ErrSchemaValidation) {
		t.Fatalf("error = %v, want ErrSchemaValidation (non-integer env)", err)
	}
}

func TestEnrollmentTokenFromEnvOnly(t *testing.T) {
	t.Setenv(config.EnvEnrollmentToken, "bzt_super_secret_value")
	cfg, err := config.Load(writeConfig(t, validYAML), discard())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.General.EnrollmentToken.Expose(); got != "bzt_super_secret_value" {
		t.Errorf("Expose() = %q, want the token", got)
	}
	if got := cfg.General.EnrollmentToken.String(); got != "***REDACTED***" {
		t.Errorf("String() = %q, want ***REDACTED***", got)
	}
	if got := fmt.Sprintf("%v / %#v", cfg.General.EnrollmentToken, cfg.General.EnrollmentToken); got != "***REDACTED*** / ***REDACTED***" {
		t.Errorf("formatting leaked the secret: %q", got)
	}
	// The token must never appear in a JSON marshal of the config.
	b, _ := json.Marshal(cfg)
	if bytes.Contains(b, []byte("bzt_super_secret_value")) {
		t.Error("enrollment token leaked into JSON marshal")
	}
}

func TestSecretRedaction(t *testing.T) {
	if config.Secret("").String() != "" {
		t.Error("empty Secret must render as empty")
	}
	if config.Secret("x").String() != "***REDACTED***" {
		t.Error("non-empty Secret must redact")
	}
	tb, _ := config.Secret("x").MarshalText()
	if string(tb) != "***REDACTED***" {
		t.Errorf("MarshalText = %q", tb)
	}
}

func TestDefaultConfigAndValidate(t *testing.T) {
	dc := config.DefaultConfig()
	if dc.Heartbeat.HeartbeatIntervalSeconds != 60 || dc.Logging.Level != "info" ||
		dc.Storage.DefaultStorageBackend != "noop" || dc.Updater.UpdateChannel != "stable" {
		t.Errorf("DefaultConfig values wrong: %+v", dc)
	}
	if dc.General.APIBaseURL != "" {
		t.Errorf("DefaultConfig.APIBaseURL = %q, want empty (no default)", dc.General.APIBaseURL)
	}
	// DefaultConfig alone is invalid (missing api_base_url).
	if err := config.Validate(dc); err == nil {
		t.Error("Validate(DefaultConfig) should fail (missing api_base_url)")
	}

	// A complete config validates.
	dc.General.APIBaseURL = "https://api.example.com"
	if err := config.Validate(dc); err != nil {
		t.Errorf("Validate(complete) = %v, want nil", err)
	}

	// nil config is invalid.
	if err := config.Validate(nil); !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("Validate(nil) = %v, want ErrInvalidConfig", err)
	}

	// Newer config_version is rejected by the semantic check.
	dc.ConfigVersion = config.CurrentConfigVersion + 1
	if err := config.Validate(dc); !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("Validate(newer version) = %v, want ErrInvalidConfig", err)
	}
}

func TestValidateSemanticURLBranch(t *testing.T) {
	// Passes the schema's https pattern but is not a parseable URL -> semantic
	// validation must reject it.
	cfg := config.DefaultConfig()
	cfg.General.APIBaseURL = "https://%zz"
	if err := config.Validate(cfg); !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("Validate(malformed url) = %v, want ErrInvalidConfig", err)
	}
}

func TestMissingAndMalformedFile(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"), discard()); !errors.Is(err, config.ErrInvalidConfig) {
		t.Errorf("Load(missing file) = %v, want ErrInvalidConfig", err)
	}
	if _, err := config.Load(writeConfig(t, "general: [this, is, not, a, map\n"), discard()); err == nil {
		t.Error("Load(malformed yaml) should error")
	}
}

func TestNilBootstrapLoggerIsSafe(t *testing.T) {
	// nil logger must not panic (Load falls back to stderr).
	if _, err := config.Load(writeConfig(t, validYAML), nil); err != nil {
		t.Fatalf("Load with nil logger: %v", err)
	}
	var l *config.BootstrapLogger
	l.Infof("must not panic")
	l.Warnf("must not panic")
}

func TestBootstrapLoggerWrites(t *testing.T) {
	var buf bytes.Buffer
	l := config.NewBootstrapLoggerTo(&buf)
	l.Infof("hello %d", 7)
	l.Warnf("careful")
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("[bootstrap][info] hello 7")) ||
		!bytes.Contains(buf.Bytes(), []byte("[bootstrap][warn] careful")) {
		t.Errorf("bootstrap logger output = %q", out)
	}
}

func TestSchemaJSONIsValid(t *testing.T) {
	b := config.SchemaJSON()
	if len(b) == 0 || !json.Valid(b) {
		t.Fatal("SchemaJSON() must return valid, non-empty JSON")
	}
	// Returned slice is a copy.
	b[0] = 'X'
	if config.SchemaJSON()[0] == 'X' {
		t.Error("SchemaJSON() must return a defensive copy")
	}
}

func TestSampleConfigIsValid(t *testing.T) {
	// Guards that the shipped sample stays loadable against the schema.
	cfg, err := config.Load(filepath.Join("..", "..", "..", "configs", "config.sample.yaml"), discard())
	if err != nil {
		t.Fatalf("configs/config.sample.yaml failed to load: %v", err)
	}
	if cfg.General.APIBaseURL != "https://api.beyzbackup.com" {
		t.Errorf("sample api_base_url = %q", cfg.General.APIBaseURL)
	}
}
