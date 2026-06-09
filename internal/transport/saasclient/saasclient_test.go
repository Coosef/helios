package saasclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

func tlsServer(t *testing.T, h http.Handler) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv, httpclient.PinFromCertificate(srv.Certificate())
}

func newClient(t *testing.T, srv *httptest.Server, pin string) *saasclient.Client {
	t.Helper()
	c, err := saasclient.New(saasclient.Options{
		BaseURL:    srv.URL,
		Pins:       []string{pin},
		HTTPConfig: &httpclient.Config{MaxRetries: 0}, // keep tests fast; retry is T12's concern
	})
	if err != nil {
		t.Fatalf("saasclient.New: %v", err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func ctx() context.Context { return context.Background() }

func TestNewValidation(t *testing.T) {
	if _, err := saasclient.New(saasclient.Options{}); err == nil {
		t.Error("New(empty base url) should error")
	}
	if _, err := saasclient.New(saasclient.Options{BaseURL: "://bad", Pins: []string{"sha256:" + strings.Repeat("ab", 32)}}); err == nil {
		t.Error("New(bad url) should error")
	}
	// Valid URL but no pins -> transport fails closed (ErrNoPins).
	if _, err := saasclient.New(saasclient.Options{BaseURL: "https://api.example.com"}); err == nil {
		t.Error("New(no pins) should fail closed")
	}
}

func TestEnrollSuccessAndTokenCached(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/v1/enroll") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Enroll is unauthenticated — no bearer token yet.
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("enroll must not carry Authorization, got %q", got)
		}
		writeJSON(w, http.StatusCreated, "application/json",
			`{"device_id":"dev_test_1","tenant_id":"tnt_1","region":"eu","agent_session_token":"ast_enroll_tok"}`)
	}))

	c := newClient(t, srv, pin)
	resp, err := c.Enroll(ctx(), proto.EnrollRequest{})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if resp.DeviceId != "dev_test_1" {
		t.Errorf("device_id = %q", resp.DeviceId)
	}
	if c.SessionToken() != "ast_enroll_tok" {
		t.Errorf("token not cached: %q", c.SessionToken())
	}
}

func TestRegisterRotatesToken(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/agents/dev_9/register") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, "application/json",
			`{"device_id":"dev_9","agent_certificate_pem":"x","agent_session_token":"ast_reg_new"}`)
	}))

	c := newClient(t, srv, pin)
	c.SetSessionToken("ast_old")
	if _, err := c.Register(ctx(), "dev_9", proto.RegisterRequest{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if c.SessionToken() != "ast_reg_new" {
		t.Errorf("token not rotated: %q", c.SessionToken())
	}
}

func TestHeartbeatTokenRotateAndPreserve(t *testing.T) {
	t.Run("rotates when server returns a token", func(t *testing.T) {
		srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, "application/json", `{"agent_session_token":"ast_hb_new"}`)
		}))
		c := newClient(t, srv, pin)
		c.SetSessionToken("ast_old")
		if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); err != nil {
			t.Fatal(err)
		}
		if c.SessionToken() != "ast_hb_new" {
			t.Errorf("token not rotated: %q", c.SessionToken())
		}
	})
	t.Run("preserves when server omits a token", func(t *testing.T) {
		srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, "application/json", `{"next_heartbeat_seconds":60}`)
		}))
		c := newClient(t, srv, pin)
		c.SetSessionToken("ast_keep")
		if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); err != nil {
			t.Fatal(err)
		}
		if c.SessionToken() != "ast_keep" {
			t.Errorf("token should be preserved: %q", c.SessionToken())
		}
	})
}

func TestRequestConstruction(t *testing.T) {
	var got http.Header
	var path string
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		path = r.URL.Path
		writeJSON(w, http.StatusOK, "application/json", `{}`)
	}))

	c := newClient(t, srv, pin)
	c.SetSessionToken("ast_req_tok")
	if _, err := c.Heartbeat(ctx(), "dev_42", proto.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(path, "/v1/agents/dev_42/heartbeat") {
		t.Errorf("path = %q, want .../v1/agents/dev_42/heartbeat", path)
	}
	if got.Get("Authorization") != "Bearer ast_req_tok" {
		t.Errorf("Authorization = %q (must use the cached token)", got.Get("Authorization"))
	}
	if got.Get("X-Agent-Version") == "" || got.Get("X-Protocol-Version") == "" {
		t.Errorf("version headers missing (T12 injection): %v", got)
	}
}

func TestUnauthorizedPropagates(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, "application/problem+json",
			`{"title":"Unauthorized","code":"AUTH_EXPIRED","detail":"session token expired"}`)
	}))
	c := newClient(t, srv, pin)
	c.SetSessionToken("ast_expired")
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); !errors.Is(err, saasclient.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestUpgradeRequiredPropagates(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUpgradeRequired, "application/problem+json",
			`{"title":"Upgrade Required","min_supported_version":3}`)
	}))
	c := newClient(t, srv, pin)
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); !errors.Is(err, saasclient.ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
}

func TestConflictPropagates(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, "application/problem+json",
			`{"title":"Conflict","code":"TOKEN_CONSUMED","detail":"enrollment token already used"}`)
	}))
	c := newClient(t, srv, pin)
	if _, err := c.Enroll(ctx(), proto.EnrollRequest{}); !errors.Is(err, saasclient.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func TestUnexpectedStatusPropagates(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, "application/problem+json", `{"title":"boom"}`)
	}))
	c := newClient(t, srv, pin)
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); !errors.Is(err, saasclient.ErrUnexpectedStatus) {
		t.Errorf("err = %v, want ErrUnexpectedStatus", err)
	}
}

func TestEmptyBodyOn2xx(t *testing.T) {
	// A 2xx with a non-JSON content-type leaves the typed body nil.
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "text/plain", "not json")
	}))
	c := newClient(t, srv, pin)
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); !errors.Is(err, saasclient.ErrEmptyBody) {
		t.Errorf("err = %v, want ErrEmptyBody", err)
	}
}

func TestNonJSONErrorBodyFallback(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadGateway, "text/plain", "upstream exploded")
	}))
	c := newClient(t, srv, pin)
	_, err := c.PollTasks(ctx(), "dev_1")
	if !errors.Is(err, saasclient.ErrUnexpectedStatus) {
		t.Fatalf("err = %v, want ErrUnexpectedStatus", err)
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error should include the raw body fallback: %v", err)
	}
}

// mutatingCall describes one Idempotency-Key-capable endpoint for table tests.
type mutatingCall struct {
	name   string
	status int
	body   string
	invoke func(c *saasclient.Client, opts ...saasclient.RequestOption) error
}

func mutatingCalls() []mutatingCall {
	return []mutatingCall{
		{
			name: "Enroll", status: http.StatusCreated,
			body: `{"device_id":"d","tenant_id":"t","region":"r","agent_session_token":"x"}`,
			invoke: func(c *saasclient.Client, opts ...saasclient.RequestOption) error {
				_, err := c.Enroll(ctx(), proto.EnrollRequest{}, opts...)
				return err
			},
		},
		{
			name: "Register", status: http.StatusOK,
			body: `{"device_id":"d","agent_certificate_pem":"x","agent_session_token":"x"}`,
			invoke: func(c *saasclient.Client, opts ...saasclient.RequestOption) error {
				_, err := c.Register(ctx(), "dev_1", proto.RegisterRequest{}, opts...)
				return err
			},
		},
		{
			name: "AckTask", status: http.StatusOK, body: `{}`,
			invoke: func(c *saasclient.Client, opts ...saasclient.RequestOption) error {
				_, err := c.AckTask(ctx(), "dev_1", "tsk_1", proto.TaskAckRequest{}, opts...)
				return err
			},
		},
		{
			name: "ReportTaskStatus", status: http.StatusOK, body: `{}`,
			invoke: func(c *saasclient.Client, opts ...saasclient.RequestOption) error {
				_, err := c.ReportTaskStatus(ctx(), "dev_1", "tsk_1", proto.TaskStatusRequest{}, opts...)
				return err
			},
		},
	}
}

func TestIdempotencyKeyThreadedWhenSupplied(t *testing.T) {
	key := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	for _, tc := range mutatingCalls() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				writeJSON(w, tc.status, "application/json", tc.body)
			}))
			c := newClient(t, srv, pin)
			if err := tc.invoke(c, saasclient.WithIdempotencyKey(key)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gk := got.Get("Idempotency-Key"); gk != key.String() {
				t.Errorf("Idempotency-Key = %q, want %q", gk, key.String())
			}
			// Rule 4/5/7: with non-nil params the generated builder writes EMPTY
			// version headers; T12 must overwrite them with the real values, and
			// there must be no duplicate header values.
			if got.Get("X-Agent-Version") == "" || got.Get("X-Protocol-Version") == "" {
				t.Errorf("version headers not injected by T12 with non-nil params: %v", got)
			}
			if v := got.Values("X-Agent-Version"); len(v) != 1 {
				t.Errorf("X-Agent-Version duplicated: %v", v)
			}
			if v := got.Values("X-Protocol-Version"); len(v) != 1 {
				t.Errorf("X-Protocol-Version duplicated: %v", v)
			}
		})
	}
}

func TestIdempotencyKeyAbsentByDefault(t *testing.T) {
	for _, tc := range mutatingCalls() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				writeJSON(w, tc.status, "application/json", tc.body)
			}))
			c := newClient(t, srv, pin)
			if err := tc.invoke(c); err != nil { // no options -> current behavior
				t.Fatalf("%s: %v", tc.name, err)
			}
			if _, ok := got["Idempotency-Key"]; ok {
				t.Errorf("Idempotency-Key must be absent by default, got %q", got.Get("Idempotency-Key"))
			}
			if got.Get("X-Agent-Version") == "" || got.Get("X-Protocol-Version") == "" {
				t.Errorf("version headers missing on default path: %v", got)
			}
		})
	}
}

// Heartbeat / PollTasks are out of scope: they carry no Idempotency-Key header.
func TestHeartbeatSendsNoIdempotencyKey(t *testing.T) {
	var got http.Header
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		writeJSON(w, http.StatusOK, "application/json", `{}`)
	}))
	c := newClient(t, srv, pin)
	if _, err := c.Heartbeat(ctx(), "dev_1", proto.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["Idempotency-Key"]; ok {
		t.Errorf("Heartbeat must not send Idempotency-Key, got %q", got.Get("Idempotency-Key"))
	}
}

// ADR-006: location_id is an advisory request hint (operator-chosen) and a
// server-authoritative response value; the agent never persists a location name.
func TestEnrollLocationIDRoundTrip(t *testing.T) {
	var reqLocation string
	var reqHadLocation bool
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if raw, ok := body["location_id"]; ok {
			reqHadLocation = true
			_ = json.Unmarshal(raw, &reqLocation)
		}
		// The server may return a DIFFERENT, authoritative location than the hint.
		writeJSON(w, http.StatusCreated, "application/json",
			`{"device_id":"dev_1","tenant_id":"tnt_1","region":"eu","location_id":"loc_site_a","agent_session_token":"x"}`)
	}))
	c := newClient(t, srv, pin)

	hint := proto.LocationId("loc_operator_hint")
	resp, err := c.Enroll(ctx(), proto.EnrollRequest{LocationId: &hint})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if !reqHadLocation || reqLocation != "loc_operator_hint" {
		t.Errorf("request location_id = %q (had=%v), want loc_operator_hint", reqLocation, reqHadLocation)
	}
	if resp.LocationId == nil || *resp.LocationId != "loc_site_a" {
		t.Errorf("response LocationId = %v, want loc_site_a", resp.LocationId)
	}
}

func TestEnrollLocationIDOmittedWhenUnset(t *testing.T) {
	var hadLocation bool
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, hadLocation = body["location_id"]
		writeJSON(w, http.StatusCreated, "application/json",
			`{"device_id":"dev_1","tenant_id":"tnt_1","region":"eu","agent_session_token":"x"}`)
	}))
	c := newClient(t, srv, pin)
	resp, err := c.Enroll(ctx(), proto.EnrollRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if hadLocation {
		t.Error("location_id must be omitted from the request when unset")
	}
	if resp.LocationId != nil {
		t.Errorf("response LocationId should be nil when the server omits it, got %v", resp.LocationId)
	}
}

func TestRegisterLocationIDParsed(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "application/json",
			`{"device_id":"dev_1","location_id":"loc_site_b","agent_certificate_pem":"x","agent_session_token":"x"}`)
	}))
	c := newClient(t, srv, pin)
	resp, err := c.Register(ctx(), "dev_1", proto.RegisterRequest{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.LocationId == nil || *resp.LocationId != "loc_site_b" {
		t.Errorf("LocationId = %v, want loc_site_b", resp.LocationId)
	}
}

func TestResponseParsing(t *testing.T) {
	srv, pin := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			writeJSON(w, http.StatusOK, "application/json",
				`{"tasks":[{"task_id":"tsk_1","type":"backup"}],"work_available":true,"next_poll_seconds":120,"server_time":"2026-06-09T00:00:00Z"}`)
		case strings.HasSuffix(r.URL.Path, "/ack"):
			writeJSON(w, http.StatusOK, "application/json", `{}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			writeJSON(w, http.StatusOK, "application/json", `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	c := newClient(t, srv, pin)

	tasks, err := c.PollTasks(ctx(), "dev_1")
	if err != nil {
		t.Fatalf("PollTasks: %v", err)
	}
	if !tasks.WorkAvailable || tasks.NextPollSeconds != 120 || len(tasks.Tasks) != 1 {
		t.Errorf("PollTasks parse wrong: %+v", tasks)
	}

	if _, err := c.AckTask(ctx(), "dev_1", "tsk_1", proto.TaskAckRequest{}); err != nil {
		t.Errorf("AckTask: %v", err)
	}
	if _, err := c.ReportTaskStatus(ctx(), "dev_1", "tsk_1", proto.TaskStatusRequest{}); err != nil {
		t.Errorf("ReportTaskStatus: %v", err)
	}
}
