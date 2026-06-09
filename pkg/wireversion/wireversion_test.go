package wireversion_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
	proto "github.com/beyzbackup/beyz-backup/pkg/proto"
	"github.com/beyzbackup/beyz-backup/pkg/wireversion"
)

func TestDefaultVersionValues(t *testing.T) {
	if wireversion.CurrentProtocolVersion != 1 {
		t.Errorf("CurrentProtocolVersion = %d, want 1", wireversion.CurrentProtocolVersion)
	}
	if wireversion.MinSupportedProtocolVersion != 1 {
		t.Errorf("MinSupportedProtocolVersion = %d, want 1", wireversion.MinSupportedProtocolVersion)
	}
	if wireversion.DefaultAgentVersion != "0.0.0-dev" {
		t.Errorf("DefaultAgentVersion = %q, want 0.0.0-dev", wireversion.DefaultAgentVersion)
	}
	if got := wireversion.ProtocolVersionHeaderValue(); got != "1" {
		t.Errorf("ProtocolVersionHeaderValue() = %q, want 1", got)
	}
}

// Simulates the ldflags-injected build var by setting buildinfo.Version, and
// verifies both the injected path and the empty-value fallback.
func TestAgentVersionFromBuildinfoAndFallback(t *testing.T) {
	orig := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = orig })

	buildinfo.Version = "1.4.2"
	if got := wireversion.AgentVersion(); got != "1.4.2" {
		t.Errorf("AgentVersion() = %q, want 1.4.2 (ldflags-injected)", got)
	}
	if got := wireversion.AgentVersionHeaderValue(); got != "1.4.2" {
		t.Errorf("AgentVersionHeaderValue() = %q, want 1.4.2", got)
	}

	buildinfo.Version = ""
	if got := wireversion.AgentVersion(); got != wireversion.DefaultAgentVersion {
		t.Errorf("AgentVersion() fallback = %q, want %q", got, wireversion.DefaultAgentVersion)
	}
}

func TestApplyHeaders(t *testing.T) {
	orig := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = orig })
	buildinfo.Version = "2.0.0"

	req, err := http.NewRequest(http.MethodGet, "https://api.beyzbackup.com/v1/agents/dev_x/tasks", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	wireversion.ApplyHeaders(req)
	if got := req.Header.Get(wireversion.HeaderAgentVersion); got != "2.0.0" {
		t.Errorf("X-Agent-Version = %q, want 2.0.0", got)
	}
	if got := req.Header.Get(wireversion.HeaderProtocolVersion); got != "1" {
		t.Errorf("X-Protocol-Version = %q, want 1", got)
	}

	// Must not panic on a nil request.
	wireversion.ApplyHeaders(nil)
}

// Proves the RequestEditor is usable by the generated client and that the
// generated Params structs accept the version values directly (alias types).
func TestRequestEditorAndGeneratedClientParams(t *testing.T) {
	// Assignability to the generated client's editor type (compile-time).
	var _ proto.RequestEditorFn = wireversion.RequestEditor()

	orig := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = orig })
	buildinfo.Version = "3.1.4"

	req, err := http.NewRequest(http.MethodPost, "https://api.beyzbackup.com/v1/enroll", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := wireversion.RequestEditor()(context.Background(), req); err != nil {
		t.Fatalf("RequestEditor: %v", err)
	}
	if got := req.Header.Get(wireversion.HeaderAgentVersion); got != "3.1.4" {
		t.Errorf("editor X-Agent-Version = %q, want 3.1.4", got)
	}
	if got := req.Header.Get(wireversion.HeaderProtocolVersion); got != "1" {
		t.Errorf("editor X-Protocol-Version = %q, want 1", got)
	}

	// Generated Params accept the version values directly (XAgentVersion is a
	// string alias, XProtocolVersion an int alias).
	params := proto.EnrollAgentParams{
		XAgentVersion:    wireversion.AgentVersionHeaderValue(),
		XProtocolVersion: wireversion.CurrentProtocolVersion,
	}
	if params.XAgentVersion != "3.1.4" {
		t.Errorf("params.XAgentVersion = %q, want 3.1.4", params.XAgentVersion)
	}
	if params.XProtocolVersion != 1 {
		t.Errorf("params.XProtocolVersion = %d, want 1", params.XProtocolVersion)
	}
}

func TestIsUpgradeRequired(t *testing.T) {
	if !wireversion.IsUpgradeRequired(&http.Response{StatusCode: http.StatusUpgradeRequired}) {
		t.Error("426 response should be detected as upgrade-required")
	}
	if wireversion.IsUpgradeRequired(&http.Response{StatusCode: http.StatusOK}) {
		t.Error("200 response must not be upgrade-required")
	}
	if wireversion.IsUpgradeRequired(nil) {
		t.Error("nil response must not be upgrade-required")
	}
}

func TestParseUpgradeRequiredWithMinFields(t *testing.T) {
	body := []byte(`{
		"type":"https://api.beyzbackup.com/problems/upgrade-required",
		"title":"Upgrade Required",
		"status":426,
		"detail":"Agent version is below the supported floor.",
		"instance":"/v1/agents/dev_01HXYZ/heartbeat",
		"code":"agent_upgrade_required",
		"min_supported_version":"1.2.0",
		"min_supported_protocol":1
	}`)
	ur, err := wireversion.ParseUpgradeRequired(body)
	if err != nil {
		t.Fatalf("ParseUpgradeRequired: %v", err)
	}
	if ur.MinSupportedVersion != "1.2.0" {
		t.Errorf("MinSupportedVersion = %q, want 1.2.0", ur.MinSupportedVersion)
	}
	if ur.MinSupportedProtocol != 1 {
		t.Errorf("MinSupportedProtocol = %d, want 1", ur.MinSupportedProtocol)
	}
	if ur.Status != 426 {
		t.Errorf("Status = %d, want 426", ur.Status)
	}
	if ur.Code != "agent_upgrade_required" {
		t.Errorf("Code = %q, want agent_upgrade_required", ur.Code)
	}
}

func TestParseUpgradeRequiredMalformed(t *testing.T) {
	if _, err := wireversion.ParseUpgradeRequired([]byte("{not valid json")); err == nil {
		t.Fatal("expected an error for a malformed problem+json body")
	}
	// A JSON value that is not an object is also rejected.
	if _, err := wireversion.ParseUpgradeRequired([]byte(`"just a string"`)); err == nil {
		t.Fatal("expected an error for a non-object body")
	}
}

func TestParseUpgradeRequiredForwardCompatible(t *testing.T) {
	body := []byte(`{
		"title":"Upgrade Required",
		"status":426,
		"min_supported_version":"2.0.0",
		"min_supported_protocol":2,
		"future_field":"whatever",
		"nested_obj":{"a":1,"b":["x","y"]}
	}`)
	ur, err := wireversion.ParseUpgradeRequired(body)
	if err != nil {
		t.Fatalf("ParseUpgradeRequired: %v", err)
	}
	if ur.MinSupportedVersion != "2.0.0" || ur.MinSupportedProtocol != 2 {
		t.Errorf("known fields wrong: %q / %d", ur.MinSupportedVersion, ur.MinSupportedProtocol)
	}
	if got, ok := ur.Extra["future_field"].(string); !ok || got != "whatever" {
		t.Errorf("unknown scalar field not preserved in Extra: %#v", ur.Extra["future_field"])
	}
	if _, ok := ur.Extra["nested_obj"]; !ok {
		t.Errorf("unknown nested field not preserved in Extra: %#v", ur.Extra)
	}
}

func TestReadUpgradeRequired(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusUpgradeRequired,
		Body:       io.NopCloser(strings.NewReader(`{"status":426,"min_supported_version":"1.5.0","min_supported_protocol":1}`)),
	}
	if !wireversion.IsUpgradeRequired(resp) {
		t.Fatal("response should be detected as 426")
	}
	ur, err := wireversion.ReadUpgradeRequired(resp)
	if err != nil {
		t.Fatalf("ReadUpgradeRequired: %v", err)
	}
	if ur.MinSupportedVersion != "1.5.0" {
		t.Errorf("MinSupportedVersion = %q, want 1.5.0", ur.MinSupportedVersion)
	}

	if _, err := wireversion.ReadUpgradeRequired(&http.Response{StatusCode: 426}); err == nil {
		t.Error("expected an error when the response body is nil")
	}
}
