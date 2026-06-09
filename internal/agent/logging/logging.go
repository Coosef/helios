// Package logging provides the agent's structured logging built on rs/zerolog.
//
// Logs are JSON by default (one object per line) with a stable field set:
//
//	ts, level, component, event, tenant_id, device_id, message
//
// The JSON sink writes to the configured file path with size/age/count rotation
// (lumberjack). A "console" format is available for development only. Every log
// line passes through a redacting writer that scrubs Authorization/Bearer
// headers and enrollment/session tokens (bzt_/ast_ prefixes), so a secret can
// never be emitted raw even if a caller logs one by mistake (req 9).
//
// This package configures logging from internal/agent/config; it does NOT
// implement the audit hash-chain (S1-T09) or any enrollment/heartbeat/HTTP
// logic.
package logging

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/rs/zerolog"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"

	"github.com/beyzbackup/beyz-backup/internal/agent/config"
)

// Standard structured-log field names.
const (
	FieldEvent     = "event"
	FieldComponent = "component"
	FieldTenantID  = "tenant_id"
	FieldDeviceID  = "device_id"
)

// DefaultComponent is used when no component is supplied.
const DefaultComponent = "agent"

const (
	formatJSON    = "json"
	formatConsole = "console"
	defaultLevel  = "info"
)

func init() {
	// Emit the timestamp under "ts" (the agreed field name); level/message keep
	// their zerolog defaults ("level"/"message").
	zerolog.TimestampFieldName = "ts"
	zerolog.TimeFieldFormat = time.RFC3339
}

// RotationConfig controls log-file rotation (lumberjack).
type RotationConfig struct {
	MaxSizeMB  int  // rotate after the file reaches this size (MB)
	MaxBackups int  // number of old files to keep
	MaxAgeDays int  // max age of an old file before deletion
	Compress   bool // gzip rotated files
}

// DefaultRotation returns sensible rotation defaults.
func DefaultRotation() RotationConfig {
	return RotationConfig{MaxSizeMB: 50, MaxBackups: 10, MaxAgeDays: 30, Compress: true}
}

// Options configures a Logger.
type Options struct {
	Level     string         // trace|debug|info|warn|error (defaults to info)
	Format    string         // "json" (default) or "console"
	FilePath  string         // JSON sink path (ignored when Writer or console is set)
	Component string         // base component (defaults to "agent")
	TenantID  string         // base tenant_id (empty pre-enrollment)
	DeviceID  string         // base device_id (empty pre-enrollment)
	Rotation  RotationConfig // file rotation (zero value -> DefaultRotation)
	// Writer overrides the destination (mainly for tests). When nil, JSON mode
	// uses a rotating file at FilePath and console mode uses stderr.
	Writer io.Writer
}

// ---- redaction --------------------------------------------------------------

var redactRules = []struct {
	re   *regexp.Regexp
	repl []byte
}{
	// "Authorization: Bearer <token>" (any token charset).
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=\-]+`), []byte("Bearer ***REDACTED***")},
	// Enrollment tokens (bzt_...) and session tokens (ast_...) anywhere.
	{regexp.MustCompile(`bzt_[A-Za-z0-9]+`), []byte("bzt_***REDACTED***")},
	{regexp.MustCompile(`ast_[A-Za-z0-9]+`), []byte("ast_***REDACTED***")},
	// JSON "authorization"/"enrollment_token" field values, regardless of scheme.
	{regexp.MustCompile(`(?i)("authorization"\s*:\s*")[^"]*`), []byte("${1}***REDACTED***")},
	{regexp.MustCompile(`(?i)("enrollment_token"\s*:\s*")[^"]*`), []byte("${1}***REDACTED***")},
}

func redactBytes(p []byte) []byte {
	for _, r := range redactRules {
		p = r.re.ReplaceAll(p, r.repl)
	}
	return p
}

// Redact returns s with known secret shapes (Bearer headers, bzt_/ast_ tokens,
// authorization/enrollment_token field values) replaced by a redaction marker.
func Redact(s string) string { return string(redactBytes([]byte(s))) }

// redactingWriter scrubs secrets from every line before it reaches the sink.
type redactingWriter struct{ w io.Writer }

func (rw redactingWriter) Write(p []byte) (int, error) {
	if _, err := rw.w.Write(redactBytes(p)); err != nil {
		return 0, err
	}
	return len(p), nil // report all input consumed (redaction may change length)
}

// ---- logger -----------------------------------------------------------------

// Logger is the agent's structured logger. It is safe for concurrent use and
// cheap to derive (WithComponent/WithIdentity).
type Logger struct {
	base      zerolog.Logger // timestamped root, without component/identity fields
	component string
	tenantID  string
	deviceID  string
	closer    io.Closer
	zl        zerolog.Logger // derived logger actually used for emitting
}

func buildWriter(opts Options) (io.Writer, io.Closer) {
	var dest io.Writer
	var closer io.Closer
	switch {
	case opts.Writer != nil:
		dest = opts.Writer
	case opts.Format == formatConsole:
		dest = os.Stderr
	default:
		rot := opts.Rotation
		if rot == (RotationConfig{}) {
			rot = DefaultRotation()
		}
		lj := &lumberjack.Logger{
			Filename:   opts.FilePath,
			MaxSize:    rot.MaxSizeMB,
			MaxBackups: rot.MaxBackups,
			MaxAge:     rot.MaxAgeDays,
			Compress:   rot.Compress,
		}
		dest, closer = lj, lj
	}
	red := redactingWriter{w: dest}
	if opts.Format == formatConsole {
		return zerolog.ConsoleWriter{Out: red, TimeFormat: time.RFC3339}, closer
	}
	return red, closer
}

// New builds a Logger from opts.
func New(opts Options) (*Logger, error) {
	if opts.Level == "" {
		opts.Level = defaultLevel
	}
	if opts.Format == "" {
		opts.Format = formatJSON
	}
	if opts.Component == "" {
		opts.Component = DefaultComponent
	}
	if opts.Format != formatJSON && opts.Format != formatConsole {
		return nil, fmt.Errorf("logging: invalid format %q (want %q or %q)", opts.Format, formatJSON, formatConsole)
	}
	lvl, err := zerolog.ParseLevel(opts.Level)
	if err != nil {
		return nil, fmt.Errorf("logging: invalid level %q: %w", opts.Level, err)
	}

	writer, closer := buildWriter(opts)
	base := zerolog.New(writer).Level(lvl).With().Timestamp().Logger()

	l := &Logger{
		base:      base,
		component: opts.Component,
		tenantID:  opts.TenantID,
		deviceID:  opts.DeviceID,
		closer:    closer,
	}
	l.rebuild()
	return l, nil
}

// NewFromConfig builds a Logger using the logging settings in cfg and the given
// component, with default rotation. device_id is set later (post-enrollment)
// via WithIdentity.
func NewFromConfig(cfg *config.Config, component string) (*Logger, error) {
	if cfg == nil {
		return nil, fmt.Errorf("logging: nil config")
	}
	return New(Options{
		Level:     cfg.Logging.Level,
		Format:    cfg.Logging.Format,
		FilePath:  cfg.Logging.FilePath,
		Component: component,
		TenantID:  cfg.General.TenantID,
		Rotation:  DefaultRotation(),
	})
}

// rebuild reconstructs the derived logger from the component/identity fields,
// ensuring each field appears exactly once.
func (l *Logger) rebuild() {
	c := l.base.With().Str(FieldComponent, l.component)
	if l.tenantID != "" {
		c = c.Str(FieldTenantID, l.tenantID)
	}
	if l.deviceID != "" {
		c = c.Str(FieldDeviceID, l.deviceID)
	}
	l.zl = c.Logger()
}

// Debug starts a debug-level event with the given event name.
func (l *Logger) Debug(event string) *zerolog.Event { return l.zl.Debug().Str(FieldEvent, event) }

// Info starts an info-level event with the given event name.
func (l *Logger) Info(event string) *zerolog.Event { return l.zl.Info().Str(FieldEvent, event) }

// Warn starts a warn-level event with the given event name.
func (l *Logger) Warn(event string) *zerolog.Event { return l.zl.Warn().Str(FieldEvent, event) }

// Error starts an error-level event with the given event name.
func (l *Logger) Error(event string) *zerolog.Event { return l.zl.Error().Str(FieldEvent, event) }

// WithComponent returns a derived Logger for a different component (the field is
// replaced, not duplicated).
func (l *Logger) WithComponent(component string) *Logger {
	cp := *l
	cp.component = component
	cp.rebuild()
	return &cp
}

// WithIdentity returns a derived Logger carrying the given tenant/device ids
// (typically set once after enrollment).
func (l *Logger) WithIdentity(tenantID, deviceID string) *Logger {
	cp := *l
	cp.tenantID = tenantID
	cp.deviceID = deviceID
	cp.rebuild()
	return &cp
}

// Zerolog returns the underlying zerolog.Logger for advanced use.
func (l *Logger) Zerolog() *zerolog.Logger { return &l.zl }

// Close closes the underlying file sink (if any). It is safe to call on derived
// or console loggers.
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}
