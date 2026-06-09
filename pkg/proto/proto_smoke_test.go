package proto_test

// Smoke tests for the generated control-plane client (S1-T03). These prove the
// generated package compiles and exposes the Sprint 1 surface; they do NOT make
// network calls and do NOT exercise transport hardening (S1-T12) or enrollment
// logic (S1-T14). The client targets a REMOTE HTTPS SaaS endpoint — there is no
// local-only server assumption (the SaaS backend ships later as Docker
// Compose / Kubernetes; see docs/DEPLOYMENT.md).

import (
	"context"
	"net/http"
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"

	proto "github.com/beyzbackup/beyz-backup/pkg/proto"
)

// Compile-time proof that the concrete clients implement the generated
// interfaces and that every Sprint 1 endpoint method exists (a missing or
// renamed method fails to compile here).
var (
	_ proto.ClientInterface              = (*proto.Client)(nil)
	_ proto.ClientWithResponsesInterface = (*proto.ClientWithResponses)(nil)

	_ = proto.ClientInterface.EnrollAgent
	_ = proto.ClientInterface.RegisterAgent
	_ = proto.ClientInterface.SendHeartbeat
	_ = proto.ClientInterface.PollTasks
	_ = proto.ClientInterface.AckTask
	_ = proto.ClientInterface.ReportTaskStatus
)

func ptr[T any](v T) *T { return &v }

func TestRecoveryPolicyConstants(t *testing.T) {
	if proto.Escrowed != "escrowed" {
		t.Errorf("Escrowed = %q, want escrowed", proto.Escrowed)
	}
	if proto.ZeroKnowledge != "zero_knowledge" {
		t.Errorf("ZeroKnowledge = %q, want zero_knowledge", proto.ZeroKnowledge)
	}
}

// Regression guard for the nullable-field contract audit: update_result must be
// a TYPED enum constrained to {ok, failed}, OPTIONAL (omission = not reporting),
// and never an unconstrained string. This compiles only if the typed enum and
// its constants exist and the field is *UpdateResult.
func TestUpdateResultIsTypedOptionalEnum(t *testing.T) {
	if proto.UpdateResultOk != "ok" || proto.UpdateResultFailed != "failed" {
		t.Fatalf("UpdateResult constants wrong: ok=%q failed=%q", proto.UpdateResultOk, proto.UpdateResultFailed)
	}

	var hr proto.HeartbeatRequest
	if hr.UpdateResult != nil {
		t.Fatal("zero-value HeartbeatRequest.UpdateResult must be nil (not reporting)")
	}
	v := proto.UpdateResultOk
	hr.UpdateResult = &v // field is *UpdateResult — typed, not *string
	if hr.UpdateResult == nil || *hr.UpdateResult != proto.UpdateResultOk {
		t.Fatal("HeartbeatRequest.UpdateResult is not a typed *UpdateResult")
	}
}

// Proves the required request/response types and fields (req 8) compile.
func TestGeneratedTypesAndFieldsCompile(t *testing.T) {
	tok := proto.EnrollmentToken("bzt_2f8c1d6e9a4b7c0d3e5f1a2b3c4d5e6f")
	req := proto.EnrollRequest{
		EnrollmentToken: &tok,
		DeviceGuid:      openapi_types.UUID{},
		CsrPem:          "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
		RecoveryPolicy:  proto.Escrowed,
		TenantId:        ptr(proto.TenantId("tnt_01ABCDEF2345")),
		ParentOrgId:     ptr(proto.ParentOrgId("msp_01QRSTU67890")),
		Region:          ptr(proto.Region("eu-central-1")),
	}
	if req.RecoveryPolicy != proto.Escrowed {
		t.Fatal("recovery_policy not set on EnrollRequest")
	}

	// Enrollment response: credential + reserved fields.
	var er proto.EnrollResponse
	_ = er.DeviceId          // server-issued opaque id
	_ = er.AgentSessionToken // bearer credential
	_ = er.TenantId
	_ = er.Region
	_ = er.ParentOrgId
	_ = er.LicenseBlob // reserved (nullable)
	_ = er.NextHeartbeatSeconds
	_ = er.NextTaskPollSeconds

	// Heartbeat response: optional token rotation (req 9) + work_available.
	var hr proto.HeartbeatResponse
	_ = hr.AgentSessionToken // *AgentSessionToken — present only when rotated
	_ = hr.WorkAvailable
	_ = hr.MinSupportedVersion
	_ = hr.NextTaskPollSeconds

	// Task polling: forward-compatible list + work_available + next_poll_seconds.
	var tr proto.TasksResponse
	_ = tr.Tasks // []TaskEnvelope
	_ = tr.WorkAvailable
	_ = tr.NextPollSeconds

	var te proto.TaskEnvelope
	_ = te.Type
	_ = te.Sequence
	_ = te.UpdateCheck // reserved rollout placeholder (UPD-5)

	// problem+json: base fields + the 426 extension members (min_supported_*).
	pj := proto.ProblemJson{
		Status: ptr(426),
		Title:  ptr("Upgrade Required"),
		Code:   ptr("agent_upgrade_required"),
		AdditionalProperties: map[string]interface{}{
			"min_supported_version":  "1.2.0",
			"min_supported_protocol": 1,
		},
	}
	if pj.AdditionalProperties["min_supported_version"] != "1.2.0" {
		t.Fatal("426 extension fields not representable on ProblemJson")
	}
}

// Proves the generated client targets an arbitrary REMOTE HTTPS endpoint.
func TestRemoteHTTPSClientConstruction(t *testing.T) {
	c, err := proto.NewClient("https://api.beyzbackup.com", proto.WithHTTPClient(http.DefaultClient))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

// Proves version headers (via Params) and a bearer token (via RequestEditorFn)
// can be attached to outbound requests, without sending anything.
func TestVersionHeadersAndBearerCanBePassed(t *testing.T) {
	const server = "https://api.beyzbackup.com"

	tok := proto.EnrollmentToken("bzt_2f8c1d6e9a4b7c0d3e5f1a2b3c4d5e6f")
	enrollReq, err := proto.NewEnrollAgentRequest(server,
		&proto.EnrollAgentParams{XAgentVersion: "1.0.0", XProtocolVersion: 1},
		proto.EnrollAgentJSONRequestBody{
			EnrollmentToken: &tok,
			DeviceGuid:      openapi_types.UUID{},
			CsrPem:          "csr",
			RecoveryPolicy:  proto.Escrowed,
		})
	if err != nil {
		t.Fatalf("NewEnrollAgentRequest: %v", err)
	}
	if got := enrollReq.Header.Get("X-Agent-Version"); got != "1.0.0" {
		t.Errorf("X-Agent-Version = %q, want 1.0.0", got)
	}
	if got := enrollReq.Header.Get("X-Protocol-Version"); got != "1" {
		t.Errorf("X-Protocol-Version = %q, want 1", got)
	}
	if enrollReq.URL.Scheme != "https" || enrollReq.URL.Host != "api.beyzbackup.com" {
		t.Errorf("expected remote https endpoint, got %s://%s", enrollReq.URL.Scheme, enrollReq.URL.Host)
	}
	if enrollReq.URL.Path != "/v1/enroll" {
		t.Errorf("path = %q, want /v1/enroll", enrollReq.URL.Path)
	}

	// Bearer agent_session_token via a RequestEditorFn (real transport hardening
	// is S1-T12; this only proves the credential can be injected).
	var bearer proto.RequestEditorFn = func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer ast_7f3a9c2e5b8d1046a2c4e6f8b0d2f4a6")
		return nil
	}
	hbReq, err := proto.NewSendHeartbeatRequest(server, proto.DeviceIdPath("dev_01HXYZ2345ABCD"),
		&proto.SendHeartbeatParams{XAgentVersion: "1.0.0", XProtocolVersion: 1},
		proto.SendHeartbeatJSONRequestBody{
			ProtocolVersion: 1,
			AgentVersion:    "1.0.0",
			Status:          proto.Idle,
		})
	if err != nil {
		t.Fatalf("NewSendHeartbeatRequest: %v", err)
	}
	if err := bearer(context.Background(), hbReq); err != nil {
		t.Fatalf("bearer editor: %v", err)
	}
	if got := hbReq.Header.Get("Authorization"); got != "Bearer ast_7f3a9c2e5b8d1046a2c4e6f8b0d2f4a6" {
		t.Errorf("Authorization = %q", got)
	}
	if got := hbReq.Header.Get("X-Agent-Version"); got != "1.0.0" {
		t.Errorf("heartbeat X-Agent-Version = %q, want 1.0.0", got)
	}
	if hbReq.URL.Path != "/v1/agents/dev_01HXYZ2345ABCD/heartbeat" {
		t.Errorf("path = %q, want /v1/agents/dev_01HXYZ2345ABCD/heartbeat", hbReq.URL.Path)
	}
}
