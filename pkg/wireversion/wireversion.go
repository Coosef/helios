// Package wireversion holds the agent/control-plane protocol version constants
// and the helpers that implement the versioning rules of ADR-004: every request
// carries X-Agent-Version and X-Protocol-Version, and the server returns
// 426 Upgrade Required (RFC 7807 problem+json) when the agent is below the
// supported floor.
//
// The package is deliberately transport-agnostic and depends only on the
// standard library and internal/buildinfo. It performs no enrollment, heartbeat,
// or transport hardening (those are separate Sprint 1 tasks). It does not import
// the generated client: the generated version-header params are string/int
// aliases that accept these values directly, and RequestEditor's return type is
// assignable to the generated RequestEditorFn.
package wireversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
)

const (
	// CurrentProtocolVersion is the control-plane protocol version this agent
	// speaks (ADR-004). It is sent as X-Protocol-Version on every request.
	CurrentProtocolVersion = 1

	// MinSupportedProtocolVersion is the lowest control-plane protocol version
	// this agent can interoperate with. It equals CurrentProtocolVersion in
	// Sprint 1 and exists so future agents can advertise a compatibility floor.
	MinSupportedProtocolVersion = 1

	// DefaultAgentVersion is the fallback agent version used when no version was
	// injected at build time (i.e. buildinfo.Version is empty). It mirrors the
	// buildinfo dev default.
	DefaultAgentVersion = "0.0.0-dev"
)

// HTTP header names for version negotiation (ADR-004).
const (
	HeaderAgentVersion    = "X-Agent-Version"
	HeaderProtocolVersion = "X-Protocol-Version"
)

// ProblemJSONMediaType is the RFC 7807 media type used for error bodies,
// including the 426 Upgrade Required response.
const ProblemJSONMediaType = "application/problem+json"

// MaxProblemBodyBytes bounds how much of a problem+json body is read by
// ReadUpgradeRequired, so a hostile or buggy server cannot force an unbounded
// read (security-first; CLAUDE.md).
const MaxProblemBodyBytes = 1 << 20 // 1 MiB

// AgentVersion returns the agent's semantic version, read from
// internal/buildinfo.Version (ldflags-injected at build time). When that value
// is empty it falls back to DefaultAgentVersion. The value is read live on each
// call so a build-time override is always reflected.
func AgentVersion() string {
	if buildinfo.Version == "" {
		return DefaultAgentVersion
	}
	return buildinfo.Version
}

// AgentVersionHeaderValue returns the value for the X-Agent-Version header.
func AgentVersionHeaderValue() string {
	return AgentVersion()
}

// ProtocolVersionHeaderValue returns the value for the X-Protocol-Version header.
func ProtocolVersionHeaderValue() string {
	return strconv.Itoa(CurrentProtocolVersion)
}

// ApplyHeaders sets the X-Agent-Version and X-Protocol-Version headers on req.
// It is a safe no-op if req or req.Header is nil.
func ApplyHeaders(req *http.Request) {
	if req == nil || req.Header == nil {
		return
	}
	req.Header.Set(HeaderAgentVersion, AgentVersionHeaderValue())
	req.Header.Set(HeaderProtocolVersion, ProtocolVersionHeaderValue())
}

// RequestEditor returns a request-editor function that stamps the version
// headers onto every outbound request. Its signature matches the generated
// client's RequestEditorFn (func(context.Context, *http.Request) error), so it
// can be passed to the generated client's WithRequestEditorFn option or to a
// per-call request editor without this package importing the generated code.
func RequestEditor() func(ctx context.Context, req *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		ApplyHeaders(req)
		return nil
	}
}

// UpgradeRequired is the parsed RFC 7807 problem+json body of a 426 Upgrade
// Required response (ADR-004). MinSupportedVersion and MinSupportedProtocol are
// RFC 7807 extension members; any other (forward-compatible) members are
// preserved in Extra.
type UpgradeRequired struct {
	Type                 string         // RFC 7807 "type"
	Title                string         // RFC 7807 "title"
	Status               int            // RFC 7807 "status"
	Detail               string         // RFC 7807 "detail"
	Instance             string         // RFC 7807 "instance"
	Code                 string         // application error code
	MinSupportedVersion  string         // extension member "min_supported_version"
	MinSupportedProtocol int            // extension member "min_supported_protocol"
	Extra                map[string]any // any other members (forward compatibility)
}

// IsUpgradeRequired reports whether resp is a 426 Upgrade Required response.
func IsUpgradeRequired(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusUpgradeRequired
}

// ParseUpgradeRequired parses an application/problem+json body (as returned for
// a 426) and extracts the standard members plus the min_supported_* extension
// fields. Unknown members are preserved in Extra (forward compatibility,
// ADR-004); a known member whose JSON type is unexpected is also preserved in
// Extra rather than failing the whole parse. It returns an error only when body
// is not a valid JSON object.
func ParseUpgradeRequired(body []byte) (*UpgradeRequired, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("wireversion: invalid problem+json body: %w", err)
	}

	ur := &UpgradeRequired{Extra: make(map[string]any)}
	for key, rv := range raw {
		switch key {
		case "type":
			putString(rv, &ur.Type, key, ur.Extra)
		case "title":
			putString(rv, &ur.Title, key, ur.Extra)
		case "status":
			putInt(rv, &ur.Status, key, ur.Extra)
		case "detail":
			putString(rv, &ur.Detail, key, ur.Extra)
		case "instance":
			putString(rv, &ur.Instance, key, ur.Extra)
		case "code":
			putString(rv, &ur.Code, key, ur.Extra)
		case "min_supported_version":
			putString(rv, &ur.MinSupportedVersion, key, ur.Extra)
		case "min_supported_protocol":
			putInt(rv, &ur.MinSupportedProtocol, key, ur.Extra)
		default:
			ur.Extra[key] = decodeAny(rv)
		}
	}
	return ur, nil
}

// ReadUpgradeRequired reads (bounded by MaxProblemBodyBytes) and parses the
// problem+json body of a 426 response. Callers should first check
// IsUpgradeRequired. It does not close resp.Body.
func ReadUpgradeRequired(resp *http.Response) (*UpgradeRequired, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("wireversion: nil response or body")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxProblemBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("wireversion: reading problem body: %w", err)
	}
	return ParseUpgradeRequired(body)
}

// putString decodes rv into dst; on a type mismatch it preserves the raw value
// in extra instead of failing.
func putString(rv json.RawMessage, dst *string, key string, extra map[string]any) {
	if err := json.Unmarshal(rv, dst); err != nil {
		extra[key] = decodeAny(rv)
	}
}

// putInt decodes rv into dst; on a type mismatch it preserves the raw value in
// extra instead of failing.
func putInt(rv json.RawMessage, dst *int, key string, extra map[string]any) {
	if err := json.Unmarshal(rv, dst); err != nil {
		extra[key] = decodeAny(rv)
	}
}

// decodeAny decodes rv into a generic value, falling back to the raw JSON string
// if it cannot be decoded.
func decodeAny(rv json.RawMessage) any {
	var v any
	if err := json.Unmarshal(rv, &v); err != nil {
		return string(rv)
	}
	return v
}
