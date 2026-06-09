package logging_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
)

func parseLine(t *testing.T, b []byte) map[string]any {
	t.Helper()
	line := bytes.TrimSpace(b)
	if !json.Valid(line) {
		t.Fatalf("log line is not valid JSON: %q", line)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}
	return m
}

func newJSON(t *testing.T, buf *bytes.Buffer, opts logging.Options) *logging.Logger {
	t.Helper()
	opts.Format = "json"
	opts.Writer = buf
	l, err := logging.New(opts)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	return l
}

func TestJSONValidityAndStandardFields(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "debug", Component: "enrollment", TenantID: "tnt_1", DeviceID: "dev_1"})
	l.Info("enroll.success").Str("extra", "x").Msg("done")

	m := parseLine(t, buf.Bytes())
	for _, f := range []string{"ts", "level", "component", "event"} {
		if _, ok := m[f]; !ok {
			t.Errorf("missing required field %q in %v", f, m)
		}
	}
	if m["level"] != "info" || m["component"] != "enrollment" || m["event"] != "enroll.success" {
		t.Errorf("standard fields wrong: %v", m)
	}
	if m["tenant_id"] != "tnt_1" || m["device_id"] != "dev_1" {
		t.Errorf("identity fields wrong: %v", m)
	}
	if m["message"] != "done" || m["extra"] != "x" {
		t.Errorf("message/extra wrong: %v", m)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "warn"})

	l.Info("low.event").Msg("should be filtered")
	if buf.Len() != 0 {
		t.Fatalf("info log emitted at warn level: %q", buf.String())
	}
	l.Warn("high.event").Msg("kept")
	m := parseLine(t, buf.Bytes())
	if m["level"] != "warn" || m["event"] != "high.event" {
		t.Errorf("warn log wrong: %v", m)
	}
}

func TestConsoleModeIsNotJSON(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.New(logging.Options{Format: "console", Level: "info", Writer: &buf})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("evt").Msg("hello world")
	out := buf.String()
	if json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Errorf("console output should not be JSON: %q", out)
	}
	if !strings.Contains(out, "hello world") || !strings.Contains(out, "evt") {
		t.Errorf("console output missing message/event: %q", out)
	}
}

func TestFileOutputAndRotationConfig(t *testing.T) {
	if logging.DefaultRotation() != (logging.RotationConfig{MaxSizeMB: 50, MaxBackups: 10, MaxAgeDays: 30, Compress: true}) {
		t.Fatalf("DefaultRotation = %+v", logging.DefaultRotation())
	}

	path := filepath.Join(t.TempDir(), "agent.log")
	l, err := logging.New(logging.Options{
		Format: "json", Level: "info", FilePath: path, Component: "agent",
		Rotation: logging.RotationConfig{MaxSizeMB: 1, MaxBackups: 2, MaxAgeDays: 7, Compress: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("file.event").Msg("to file")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	m := parseLine(t, data)
	if m["event"] != "file.event" || m["message"] != "to file" {
		t.Errorf("file log wrong: %v", m)
	}
}

func TestRedactionScrubsSecrets(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "info"})
	l.Info("auth").
		Str("header", "Bearer ast_sessionSECRET1").
		Str("session", "ast_bareSESSION2").
		Str("token", "bzt_enrollSECRET3").
		Str("authorization", "Basic ZGVhZGJlZWZjcmVkcw==").
		Msg("attempt bzt_msgleak4")

	out := buf.String()
	for _, raw := range []string{"ast_sessionSECRET1", "ast_bareSESSION2", "bzt_enrollSECRET3", "bzt_msgleak4", "ZGVhZGJlZWZjcmVkcw=="} {
		if strings.Contains(out, raw) {
			t.Errorf("raw secret %q leaked into log: %s", raw, out)
		}
	}
	for _, marker := range []string{"Bearer ***REDACTED***", "ast_***REDACTED***", "bzt_***REDACTED***", `"authorization":"***REDACTED***"`} {
		if !strings.Contains(out, marker) {
			t.Errorf("expected redaction marker %q in: %s", marker, out)
		}
	}
	// Output must remain valid JSON after redaction.
	parseLine(t, buf.Bytes())
}

func TestNoRawTokenLeakageAnywhere(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "info"})
	// Try to leak a token via the event name, a field, and the message.
	l.Info("got bzt_eventLeak").Str("k", "ast_fieldLeak").Msg("msg bzt_msgLeak")
	out := buf.String()
	for _, raw := range []string{"bzt_eventLeak", "ast_fieldLeak", "bzt_msgLeak"} {
		if strings.Contains(out, raw) {
			t.Errorf("token %q leaked: %s", raw, out)
		}
	}
}

func TestRedactFunction(t *testing.T) {
	if got := logging.Redact("Authorization: Bearer abc.def-123"); strings.Contains(got, "abc.def-123") || !strings.Contains(got, "Bearer ***REDACTED***") {
		t.Errorf("Redact(bearer) = %q", got)
	}
	if got := logging.Redact("bzt_token"); got != "bzt_***REDACTED***" {
		t.Errorf("Redact(bzt) = %q", got)
	}
	if got := logging.Redact("ast_token"); got != "ast_***REDACTED***" {
		t.Errorf("Redact(ast) = %q", got)
	}
	if got := logging.Redact("nothing secret here"); got != "nothing secret here" {
		t.Errorf("Redact(plain) = %q", got)
	}
}

func TestWithComponentReplacesNotDuplicates(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "info", Component: "agent"})
	l.WithComponent("heartbeat").Info("hb.sent").Msg("m")

	out := buf.String()
	if strings.Count(out, `"component"`) != 1 {
		t.Errorf("component field should appear exactly once: %s", out)
	}
	if m := parseLine(t, buf.Bytes()); m["component"] != "heartbeat" {
		t.Errorf("component = %v, want heartbeat", m["component"])
	}
}

func TestWithIdentity(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "info"})

	l.WithIdentity("tnt_9", "dev_9").Info("x").Msg("m")
	m := parseLine(t, buf.Bytes())
	if m["tenant_id"] != "tnt_9" || m["device_id"] != "dev_9" {
		t.Errorf("identity wrong: %v", m)
	}

	// The base logger must not carry identity fields.
	buf.Reset()
	l.Info("y").Msg("m")
	m2 := parseLine(t, buf.Bytes())
	if _, ok := m2["tenant_id"]; ok {
		t.Errorf("base logger leaked tenant_id: %v", m2)
	}
}

func TestDebugAndErrorLevels(t *testing.T) {
	var buf bytes.Buffer
	l := newJSON(t, &buf, logging.Options{Level: "debug"})

	l.Debug("d.event").Msg("dbg")
	if m := parseLine(t, buf.Bytes()); m["level"] != "debug" || m["event"] != "d.event" {
		t.Errorf("debug log wrong: %v", m)
	}
	buf.Reset()
	l.Error("e.event").Msg("err")
	if m := parseLine(t, buf.Bytes()); m["level"] != "error" || m["event"] != "e.event" {
		t.Errorf("error log wrong: %v", m)
	}
}

func TestFileDefaultRotationWhenUnset(t *testing.T) {
	// No Rotation in Options -> buildWriter must apply DefaultRotation.
	path := filepath.Join(t.TempDir(), "default-rot.log")
	l, err := logging.New(logging.Options{Format: "json", Level: "info", FilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("rot.default").Msg("ok")
	_ = l.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if m := parseLine(t, data); m["event"] != "rot.default" {
		t.Errorf("default-rotation file log wrong: %v", m)
	}
}

func TestInvalidOptions(t *testing.T) {
	var buf bytes.Buffer
	if _, err := logging.New(logging.Options{Level: "bogus", Format: "json", Writer: &buf}); err == nil {
		t.Error("invalid level should error")
	}
	if _, err := logging.New(logging.Options{Format: "xml", Writer: &buf}); err == nil {
		t.Error("invalid format should error")
	}
}

func TestNewFromConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fromcfg.log")
	cfg := config.DefaultConfig()
	cfg.General.APIBaseURL = "https://api.example.com"
	cfg.General.TenantID = "tnt_cfg"
	cfg.Logging.FilePath = path
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"

	l, err := logging.NewFromConfig(cfg, "agent")
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	l.Info("cfg.event").Msg("from config")
	_ = l.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := parseLine(t, data)
	if m["component"] != "agent" || m["tenant_id"] != "tnt_cfg" || m["event"] != "cfg.event" {
		t.Errorf("config-driven log wrong: %v", m)
	}

	if _, err := logging.NewFromConfig(nil, "agent"); err == nil {
		t.Error("NewFromConfig(nil) should error")
	}
}

func TestCloseAndZerolog(t *testing.T) {
	var buf bytes.Buffer
	l, err := logging.New(logging.Options{Format: "console", Writer: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if l.Zerolog() == nil {
		t.Error("Zerolog() returned nil")
	}
	if err := l.Close(); err != nil { // no file closer -> nil
		t.Errorf("Close (no closer) = %v", err)
	}
}
