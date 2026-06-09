// Package config loads, validates, and types the Beyz Backup agent
// configuration (config.yaml).
//
// Loading precedence is: built-in defaults < config.yaml < environment variables
// (prefix BEYZ_). The merged result is validated against an embedded JSON Schema
// (unknown fields, enums, bounds, required fields, types) and then against a few
// programmatic semantic rules (https-only API URL, supported config version).
// The result is a typed, treat-as-immutable *Config.
//
// SECURITY: secrets are never stored in config.yaml. The single-use enrollment
// token is read ONLY from the BEYZ_ENROLLMENT_TOKEN environment variable (or, at
// enrollment, the installer one-shot file); the schema's additionalProperties
// rejects an enrollment_token placed in config.yaml. The token is held in a
// redacting Secret type so it cannot be accidentally logged.
//
// This package depends on koanf + jsonschema and a minimal stderr bootstrap
// logger; it does NOT depend on the structured logging subsystem (S1-T08).
package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CurrentConfigVersion is the highest config_version this build understands.
const CurrentConfigVersion = 1

// Environment-variable conventions.
const (
	// EnvPrefix prefixes all config-overriding environment variables.
	EnvPrefix = "BEYZ_"
	// EnvEnrollmentToken is the env var carrying the single-use enrollment token.
	// It is intentionally NOT a config.yaml field (SEC-2).
	EnvEnrollmentToken = EnvPrefix + "ENROLLMENT_TOKEN"
)

const schemaID = "config.schema.json"

// Built-in defaults (applied beneath config.yaml and env).
const (
	defaultHeartbeatIntervalSeconds   = 60
	defaultTaskPollIntervalSeconds    = 300
	defaultLogLevel                   = "info"
	defaultLogFormat                  = "json"
	defaultStorageBackend             = "noop"
	defaultStorageTimeoutSeconds      = 60
	defaultUpdateChannel              = "stable"
	defaultUpdateCheckIntervalSeconds = 3600
)

// Sentinel errors. Match with errors.Is.
var (
	// ErrInvalidConfig indicates a configuration that failed validation
	// (semantic rules, unreadable file, unsupported version, ...).
	ErrInvalidConfig = errors.New("config: invalid configuration")
	// ErrSchemaValidation indicates the configuration failed JSON-Schema
	// validation (unknown field, bad enum/bound/type, missing required field).
	ErrSchemaValidation = errors.New("config: schema validation failed")
)

//go:embed config.schema.json
var schemaJSON []byte

var compiledSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	sch, err := compileSchema(schemaJSON)
	if err != nil {
		panic("config: embedded schema: " + err.Error())
	}
	return sch
}

func compileSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaID, doc); err != nil {
		return nil, fmt.Errorf("adding schema resource: %w", err)
	}
	sch, err := c.Compile(schemaID)
	if err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}
	return sch, nil
}

// Secret is a string whose value is redacted by String/format/JSON so it cannot
// be accidentally logged. Use Expose to obtain the underlying value.
type Secret string

// String returns a redaction placeholder (or "" when empty).
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return "***REDACTED***"
}

// GoString redacts in %#v formatting.
func (s Secret) GoString() string { return s.String() }

// MarshalText redacts the value for text/JSON marshaling.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Expose returns the underlying secret value. Call only where the raw value is
// genuinely required (e.g. building the enrollment request); never log it.
func (s Secret) Expose() string { return string(s) }

// Config is the typed, validated agent configuration. Treat it as immutable
// after Load: callers must not mutate it.
type Config struct {
	ConfigVersion int       `json:"config_version"`
	General       General   `json:"general"`
	Heartbeat     Heartbeat `json:"heartbeat"`
	Logging       Logging   `json:"logging"`
	Storage       Storage   `json:"storage"`
	Updater       Updater   `json:"updater"`
}

// General holds top-level identity/endpoint settings.
type General struct {
	TenantID   string `json:"tenant_id"`
	Region     string `json:"region"`
	APIBaseURL string `json:"api_base_url"`
	// EnrollmentToken is a SECRET sourced only from BEYZ_ENROLLMENT_TOKEN; it is
	// excluded from (de)serialization (json:"-") so it is never read from or
	// written to config.yaml.
	EnrollmentToken Secret `json:"-"`
}

// Heartbeat holds presence/poll cadence floors (the server may override at runtime).
type Heartbeat struct {
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
	TaskPollIntervalSeconds  int `json:"task_poll_interval_seconds"`
}

// Logging holds structured-logging settings (consumed by S1-T08).
type Logging struct {
	Level    string `json:"level"`
	Format   string `json:"format"`
	FilePath string `json:"file_path"`
}

// Storage holds storage-target defaults (only "noop" is wired in Sprint 1).
type Storage struct {
	DefaultStorageBackend string `json:"default_storage_backend"`
	StorageTimeoutSeconds int    `json:"storage_timeout_seconds"`
}

// Updater holds update-channel settings.
type Updater struct {
	UpdateChannel              string `json:"update_channel"`
	UpdateCheckIntervalSeconds int    `json:"update_check_interval_seconds"`
}

// SchemaJSON returns a copy of the embedded JSON Schema.
func SchemaJSON() []byte { return append([]byte(nil), schemaJSON...) }

func defaultLogPath() string { return defaultLogPathFor(runtime.GOOS) }

func defaultLogPathFor(goos string) string {
	if goos == "windows" {
		return `C:\ProgramData\BeyzBackup\logs\agent.log`
	}
	return "/var/log/beyz-backup/agent.log"
}

// DefaultConfig returns the built-in defaults. Note that General.APIBaseURL is
// intentionally empty: it has no default and must be supplied via config.yaml or
// BEYZ_GENERAL_API_BASE_URL, so DefaultConfig alone does not pass Validate.
func DefaultConfig() *Config {
	return &Config{
		ConfigVersion: CurrentConfigVersion,
		General:       General{},
		Heartbeat: Heartbeat{
			HeartbeatIntervalSeconds: defaultHeartbeatIntervalSeconds,
			TaskPollIntervalSeconds:  defaultTaskPollIntervalSeconds,
		},
		Logging: Logging{
			Level:    defaultLogLevel,
			Format:   defaultLogFormat,
			FilePath: defaultLogPath(),
		},
		Storage: Storage{
			DefaultStorageBackend: defaultStorageBackend,
			StorageTimeoutSeconds: defaultStorageTimeoutSeconds,
		},
		Updater: Updater{
			UpdateChannel:              defaultUpdateChannel,
			UpdateCheckIntervalSeconds: defaultUpdateCheckIntervalSeconds,
		},
	}
}

// defaultsConfmap is the koanf default layer. It deliberately omits
// general.api_base_url (required, no default) so an omitted value is reported as
// a missing required field, and omits the enrollment token (secret).
func defaultsConfmap() map[string]interface{} {
	return map[string]interface{}{
		"config_version": CurrentConfigVersion,
		"general": map[string]interface{}{
			"tenant_id": "",
			"region":    "",
		},
		"heartbeat": map[string]interface{}{
			"heartbeat_interval_seconds": defaultHeartbeatIntervalSeconds,
			"task_poll_interval_seconds": defaultTaskPollIntervalSeconds,
		},
		"logging": map[string]interface{}{
			"level":     defaultLogLevel,
			"format":    defaultLogFormat,
			"file_path": defaultLogPath(),
		},
		"storage": map[string]interface{}{
			"default_storage_backend": defaultStorageBackend,
			"storage_timeout_seconds": defaultStorageTimeoutSeconds,
		},
		"updater": map[string]interface{}{
			"update_channel":                defaultUpdateChannel,
			"update_check_interval_seconds": defaultUpdateCheckIntervalSeconds,
		},
	}
}

// configKeyPaths are the dotted koanf paths of every config leaf. Used to build
// the environment-variable mapping (BEYZ_<UPPER_SNAKE> -> dotted path).
var configKeyPaths = []string{
	"config_version",
	"general.tenant_id", "general.region", "general.api_base_url",
	"heartbeat.heartbeat_interval_seconds", "heartbeat.task_poll_interval_seconds",
	"logging.level", "logging.format", "logging.file_path",
	"storage.default_storage_backend", "storage.storage_timeout_seconds",
	"updater.update_channel", "updater.update_check_interval_seconds",
}

// integerKeyPaths are the leaf paths whose values must be integers.
var integerKeyPaths = map[string]bool{
	"config_version":                       true,
	"heartbeat.heartbeat_interval_seconds": true,
	"heartbeat.task_poll_interval_seconds": true,
	"storage.storage_timeout_seconds":      true,
	"updater.update_check_interval_seconds": true,
}

// envKeyMap maps the upper-snake env suffix (after the BEYZ_ prefix) to the
// dotted koanf key, e.g. "GENERAL_API_BASE_URL" -> "general.api_base_url".
func envKeyMap() map[string]string {
	m := make(map[string]string, len(configKeyPaths))
	for _, k := range configKeyPaths {
		m[strings.ToUpper(strings.ReplaceAll(k, ".", "_"))] = k
	}
	return m
}

// Load reads, merges (defaults < file < env), validates, and types the
// configuration. The config file at path must exist. boot may be nil.
func Load(path string, boot *BootstrapLogger) (*Config, error) {
	if boot == nil {
		boot = NewBootstrapLogger()
	}

	k := koanf.New(".")

	// confmap is an in-memory provider over a static map; its load cannot fail.
	_ = k.Load(confmap.Provider(defaultsConfmap(), "."), nil)

	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("%w: cannot read config file %q: %v", ErrInvalidConfig, path, err)
		}
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("%w: parsing %q: %v", ErrInvalidConfig, path, err)
		}
	}

	em := envKeyMap()
	envProvider := env.ProviderWithValue(EnvPrefix, ".", func(name, value string) (string, interface{}) {
		key, ok := em[strings.TrimPrefix(name, EnvPrefix)]
		if !ok {
			return "", nil // ignore unknown BEYZ_ vars (incl. the enrollment token)
		}
		if integerKeyPaths[key] {
			if n, err := strconv.Atoi(value); err == nil {
				return key, n
			}
			// Leave as string so schema validation reports the type error.
		}
		return key, value
	})
	// env is an in-memory provider over os.Environ(); its load cannot fail.
	_ = k.Load(envProvider, nil)

	merged, err := json.Marshal(k.Raw())
	if err != nil {
		return nil, fmt.Errorf("config: serializing merged config: %w", err)
	}
	if err := validateAgainstSchema(merged); err != nil {
		return nil, err
	}

	cfg, err := decodeConfig(merged)
	if err != nil {
		return nil, err
	}

	// Secret: only from the environment, never from the file/schema.
	if tok := os.Getenv(EnvEnrollmentToken); tok != "" {
		cfg.General.EnrollmentToken = Secret(tok)
	}

	if err := validateSemantics(cfg); err != nil {
		return nil, err
	}

	boot.Infof("configuration loaded from %q (%d keys merged)", path, len(k.Keys()))
	return cfg, nil
}

// Validate checks a Config against the JSON Schema and the semantic rules. It is
// useful for validating a programmatically-built Config. (Unknown-field
// rejection only applies to Load, since a struct cannot carry unknown fields.)
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: nil config", ErrInvalidConfig)
	}
	b, err := json.Marshal(cfg) // EnrollmentToken excluded via json:"-"
	if err != nil {
		return fmt.Errorf("config: serializing config: %w", err)
	}
	if err := validateAgainstSchema(b); err != nil {
		return err
	}
	return validateSemantics(cfg)
}

// decodeConfig decodes schema-validated JSON into a Config. It can still fail
// when a value is valid per the schema but not representable in Go (e.g. an
// integer larger than the platform int).
func decodeConfig(merged []byte) (*Config, error) {
	cfg := &Config{}
	if err := json.Unmarshal(merged, cfg); err != nil {
		return nil, fmt.Errorf("%w: decoding merged config: %v", ErrInvalidConfig, err)
	}
	return cfg, nil
}

func validateAgainstSchema(jsonBytes []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := compiledSchema.Validate(inst); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaValidation, err)
	}
	return nil
}

func validateSemantics(cfg *Config) error {
	if cfg.ConfigVersion > CurrentConfigVersion {
		return fmt.Errorf("%w: config_version %d is newer than the supported version %d; upgrade the agent",
			ErrInvalidConfig, cfg.ConfigVersion, CurrentConfigVersion)
	}
	u, err := url.Parse(cfg.General.APIBaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: general.api_base_url must be a valid https:// URL, got %q",
			ErrInvalidConfig, cfg.General.APIBaseURL)
	}
	return nil
}

// BootstrapLogger is a minimal stderr logger used ONLY during early startup
// (config load) before the structured logging subsystem (S1-T08) exists.
type BootstrapLogger struct {
	w io.Writer
}

// NewBootstrapLogger returns a bootstrap logger writing to stderr.
func NewBootstrapLogger() *BootstrapLogger { return &BootstrapLogger{w: os.Stderr} }

// NewBootstrapLoggerTo returns a bootstrap logger writing to w (for tests).
func NewBootstrapLoggerTo(w io.Writer) *BootstrapLogger { return &BootstrapLogger{w: w} }

// Infof logs an informational line.
func (l *BootstrapLogger) Infof(format string, a ...any) { l.logf("info", format, a...) }

// Warnf logs a warning line.
func (l *BootstrapLogger) Warnf(format string, a ...any) { l.logf("warn", format, a...) }

func (l *BootstrapLogger) logf(level, format string, a ...any) {
	if l == nil || l.w == nil {
		return
	}
	fmt.Fprintf(l.w, "[bootstrap][%s] %s\n", level, fmt.Sprintf(format, a...))
}
