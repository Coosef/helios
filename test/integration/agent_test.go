package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/heartbeat"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/agent/tasks"
	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
)

// agentKit wires the REAL agent use-cases (identity/enroll/heartbeat/tasks) over a
// real state store + a real, SPKI-pinned saasclient against the fake SaaS.
type agentKit struct {
	f      *fakeSaaS
	cfg    *config.Config
	store  *state.Store
	client *saasclient.Client
	em     *audit.Emitter
}

func newAgentKit(t *testing.T, f *fakeSaaS, srv *httptest.Server, pin string) *agentKit {
	t.Helper()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(state.Options{Dir: t.TempDir(), Protector: prot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	client, err := saasclient.New(saasclient.Options{
		BaseURL: srv.URL, Pins: []string{pin}, HTTPConfig: &httpclient.Config{MaxRetries: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	em, err := audit.New(audit.NewMemoryAppender(), nil, audit.Identity{Source: audit.SourceAgent, DeviceGUID: "g"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{General: config.General{
		APIBaseURL: srv.URL, TenantID: "tnt_int", Region: "eu-central-1",
		EnrollmentToken: config.Secret("bzt_2f8c1d6e9a4b7c0d3e5f1a2b3c4d5e6f"),
	}, Heartbeat: config.Heartbeat{HeartbeatIntervalSeconds: 60, TaskPollIntervalSeconds: 300}}
	return &agentKit{f: f, cfg: cfg, store: store, client: client, em: em}
}

func (k *agentKit) enroller(t *testing.T) *enroll.Enroller {
	t.Helper()
	idm, err := identity.New(k.store)
	if err != nil {
		t.Fatal(err)
	}
	e, err := enroll.New(enroll.Deps{Config: k.cfg, Identity: idm, Client: k.client, State: k.store, Audit: k.em})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// midpoint jitter source (0.5) -> no jitter, so cadence == the server value exactly.
func midRand() float64 { return 0.5 }

func (k *agentKit) beater(t *testing.T, rnd func() float64) *heartbeat.Beater {
	t.Helper()
	b, err := heartbeat.New(heartbeat.Deps{
		Config: k.cfg, Client: k.client, State: k.store, Audit: k.em,
		Now:   func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Sleep: func(context.Context, time.Duration) error { return nil }, // never really sleep
		Rand:  rnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (k *agentKit) poller(t *testing.T, rnd func() float64) *tasks.Poller {
	t.Helper()
	p, err := tasks.New(tasks.Deps{
		Config: k.cfg, Client: k.client, State: k.store, Audit: k.em,
		Now:  func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Wait: func(context.Context, time.Duration) (bool, error) { return false, nil },
		Rand: rnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- AC-14: enroll persists the credential ----------------------------------

func TestIntegrationEnrollPersistsCredential(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)

	res, err := k.enroller(t).Enroll(context.Background())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if res.DeviceID != "dev_int_1" || res.TenantID != "tnt_int" {
		t.Errorf("result = %+v", res)
	}
	// Server-authoritative values persisted to the protected store.
	if got, _ := k.store.Get(state.KeyDeviceID); string(got) != "dev_int_1" {
		t.Errorf("device_id not persisted: %q", got)
	}
	if got, _ := k.store.Get(state.KeyCertificate); len(got) == 0 || !strings.Contains(string(got), "CERTIFICATE") {
		t.Error("agent certificate not persisted")
	}
	if tok, _ := k.store.GetSecret(state.SecretSessionToken); string(tok) != "ast_enroll_1" {
		t.Errorf("session token not persisted: %q", tok)
	}
	// AC-14: the token went in the BODY, never the URL/query.
	req, ok := f.lastTo("/v1/enroll")
	if !ok {
		t.Fatal("no enroll request captured")
	}
	if strings.Contains(req.RawQuery, "bzt_") || strings.Contains(req.Path, "bzt_") {
		t.Error("enrollment token leaked into the URL")
	}
	// The token VALUE (not just the field name) must be in the body.
	if !strings.Contains(req.Body, string(k.cfg.General.EnrollmentToken)) {
		t.Errorf("enrollment token value not in the request body: %s", req.Body)
	}
}

// ---- AC-15: token reuse -> 409, fail closed, state intact -------------------

func TestIntegrationEnrollTokenReuse409(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	certBefore, _ := k.store.Get(state.KeyCertificate)

	f.mu.Lock()
	f.enrollStatus = 409 // server now reports the token consumed
	f.mu.Unlock()

	_, err := k.enroller(t).Enroll(context.Background())
	if err == nil {
		t.Fatal("re-enroll with a consumed token must fail")
	}
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Errorf("err = %v, want ErrTokenRejected", err)
	}
	if certAfter, _ := k.store.Get(state.KeyCertificate); string(certAfter) != string(certBefore) {
		t.Error("existing enrolled state was overwritten on a 409")
	}
}

// ---- register ----------------------------------------------------------------

func TestIntegrationRegisterRenews(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.client.Register(context.Background(), "dev_int_1", protoRegisterReq()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := f.lastTo("/register"); !ok {
		t.Error("register request not observed")
	}
}

// ---- AC-18: heartbeat headers + minimal body --------------------------------

func TestIntegrationHeartbeatHeadersAndBody(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.beater(t, midRand).Beat(context.Background()); err != nil {
		t.Fatalf("beat: %v", err)
	}
	req, ok := f.lastTo("/heartbeat")
	if !ok {
		t.Fatal("no heartbeat captured")
	}
	if req.Header.Get("X-Agent-Version") == "" || req.Header.Get("X-Protocol-Version") == "" {
		t.Errorf("missing version headers: %v", req.Header)
	}
	// Parse the body and assert structurally: version/status present, NO task data.
	var body map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		t.Fatalf("heartbeat body not JSON: %v", err)
	}
	if _, ok := body["agent_version"]; !ok {
		t.Error("heartbeat body missing agent_version")
	}
	if _, ok := body["status"]; !ok {
		t.Error("heartbeat body missing status")
	}
	for _, k := range []string{"tasks", "task", "task_envelope"} {
		if _, ok := body[k]; ok {
			t.Errorf("heartbeat body must not carry task data (AC-18): has %q", k)
		}
	}
}

// ---- AC-19: server-controlled heartbeat cadence + jitter --------------------

func TestIntegrationHeartbeatCadence(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.nextHeartbeatSeconds = 90 // server-directed cadence (differs from the 60s config floor)
	f.mu.Unlock()

	// midpoint jitter -> exactly the server value.
	res, err := k.beater(t, midRand).Beat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.NextInterval != 90*time.Second {
		t.Errorf("cadence = %v, want server-directed 90s (not the 60s config floor)", res.NextInterval)
	}
	// extreme jitter still lands within ±20% of the server value, never zero.
	for _, r := range []func() float64{func() float64 { return 0 }, func() float64 { return 0.999 }} {
		got, err := k.beater(t, r).Beat(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.NextInterval < 72*time.Second || got.NextInterval > 108*time.Second || got.NextInterval == 0 {
			t.Errorf("jittered cadence %v out of [72s,108s]", got.NextInterval)
		}
	}
}

// ---- AC-21: task poll empty + cadence ---------------------------------------

func TestIntegrationTaskPollEmptyAndCadence(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.nextPollSeconds = 200
	f.mu.Unlock()

	res, err := k.poller(t, midRand).PollOnce(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if res.NextInterval != 200*time.Second {
		t.Errorf("poll cadence = %v, want 200s", res.NextInterval)
	}
	req, ok := f.lastTo("/tasks")
	if !ok || req.Method != "GET" {
		t.Errorf("tasks request = %+v", req)
	}
	if req.Header.Get("X-Agent-Version") == "" {
		t.Error("task poll missing version headers")
	}
}

// ---- AC-20: 426 protocol floor + 401 auth failure ---------------------------

func TestIntegrationHeartbeat426And401(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.heartbeatStatus = 426
	f.mu.Unlock()
	if _, err := k.beater(t, midRand).Beat(context.Background()); !errors.Is(err, heartbeat.ErrUpgradeRequired) {
		t.Errorf("426: err = %v, want ErrUpgradeRequired", err)
	}
	f.mu.Lock()
	f.heartbeatStatus = 401
	f.mu.Unlock()
	if _, err := k.beater(t, midRand).Beat(context.Background()); !errors.Is(err, heartbeat.ErrUnauthorized) {
		t.Errorf("401: err = %v, want ErrUnauthorized", err)
	}
}

// ---- AC-22: forward-compatible reserved/unknown fields ----------------------

func TestIntegrationForwardCompatReservedFields(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, f, srv, pin)
	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.extraHeartbeatJSON = `,"hold_seconds":5,"schema_version":99,"some_future_field":{"x":1}`
	f.extraTasksJSON = `,"work_available":true,"rollout_cohort_pct":10,"schema_version":99,"unknown":[1,2]`
	f.mu.Unlock()

	if _, err := k.beater(t, midRand).Beat(context.Background()); err != nil {
		t.Errorf("heartbeat with reserved fields must parse: %v", err)
	}
	res, err := k.poller(t, midRand).PollOnce(context.Background())
	if err != nil {
		t.Errorf("tasks with reserved fields must parse: %v", err)
	}
	if res != nil && !res.WorkAvailable {
		t.Error("work_available=true should be honored")
	}
}

// ---- AC-34: SPKI pin mismatch refused ---------------------------------------

func TestIntegrationPinMismatchRefused(t *testing.T) {
	f, srv, _ := newFakeSaaS(t)
	_ = f
	// A client pinned to a DIFFERENT key than the server presents.
	wrongPin := "sha256:" + strings.Repeat("ab", 32)
	bad, err := saasclient.New(saasclient.Options{
		BaseURL: srv.URL, Pins: []string{wrongPin}, HTTPConfig: &httpclient.Config{MaxRetries: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bad.Heartbeat(context.Background(), "dev_int_1", protoHeartbeatReq())
	if err == nil {
		t.Fatal("a non-pinned server certificate must be refused (AC-34)")
	}
	// Refused BY THE PINNING LOGIC, not some unrelated transport error.
	if !errors.Is(err, httpclient.ErrPinMismatch) {
		t.Errorf("err = %v, want ErrPinMismatch (AC-34)", err)
	}
}
