package heartbeat_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/heartbeat"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

func ctx() context.Context { return context.Background() }

// ---- fakes ------------------------------------------------------------------

type fakeClient struct {
	seeded  string
	fn      func(call int, deviceID string, body proto.HeartbeatRequest) (*proto.HeartbeatResponse, error)
	calls   int
	lastReq proto.HeartbeatRequest
	lastDev string
}

func (c *fakeClient) SetSessionToken(t string) { c.seeded = t }

func (c *fakeClient) Heartbeat(_ context.Context, deviceID string, body proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	c.lastReq, c.lastDev = body, deviceID
	n := c.calls
	c.calls++
	return c.fn(n, deviceID, body)
}

type fakeState struct {
	plain        map[string][]byte
	secret       map[string][]byte
	putSecretErr error
}

func newFakeState(enrolled bool) *fakeState {
	s := &fakeState{plain: map[string][]byte{}, secret: map[string][]byte{}}
	if enrolled {
		s.plain[state.KeyDeviceID] = []byte("dev_hb1")
		s.plain[state.KeyCertificate] = []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n")
		s.secret[state.SecretSessionToken] = []byte("ast_initial0001")
	}
	return s
}

func (s *fakeState) Get(k string) ([]byte, error) {
	if v, ok := s.plain[k]; ok {
		return v, nil
	}
	return nil, state.ErrNotFound
}

func (s *fakeState) GetSecret(k string) ([]byte, error) {
	if v, ok := s.secret[k]; ok {
		return v, nil
	}
	return nil, state.ErrNotFound
}

func (s *fakeState) PutSecret(k string, v []byte) error {
	if s.putSecretErr != nil {
		return s.putSecretErr
	}
	s.secret[k] = append([]byte(nil), v...)
	return nil
}

type fakeAudit struct {
	events []audit.Event
}

func (a *fakeAudit) Emit(ev audit.Event) (audit.Record, error) {
	a.events = append(a.events, ev)
	return audit.Record{}, nil
}

func (a *fakeAudit) types() []string {
	out := make([]string, 0, len(a.events))
	for _, e := range a.events {
		out = append(out, e.EventType)
	}
	return out
}

// ---- builders ---------------------------------------------------------------

// okResp builds a heartbeat response with the given server cadence; no rotation.
func okResp(nextSeconds int) *proto.HeartbeatResponse {
	return &proto.HeartbeatResponse{NextHeartbeatSeconds: nextSeconds}
}

func always(resp *proto.HeartbeatResponse, err error) func(int, string, proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
	return func(int, string, proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) { return resp, err }
}

func newBeater(t *testing.T, st *fakeState, cl *fakeClient, au *fakeAudit, mut func(*heartbeat.Deps)) *heartbeat.Beater {
	t.Helper()
	d := heartbeat.Deps{
		Config: &config.Config{Heartbeat: config.Heartbeat{HeartbeatIntervalSeconds: 60, TaskPollIntervalSeconds: 300}},
		Client: cl, State: st, Audit: au,
		Now:   func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Rand:  func() float64 { return 0.5 }, // midpoint -> no jitter
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	if mut != nil {
		mut(&d)
	}
	b, err := heartbeat.New(d)
	if err != nil {
		t.Fatalf("heartbeat.New: %v", err)
	}
	return b
}

// ---- enrolled predicate -----------------------------------------------------

func TestNewEnrolledPredicate(t *testing.T) {
	cl := &fakeClient{fn: always(okResp(120), nil)}
	st := newFakeState(true)
	b := newBeater(t, st, cl, &fakeAudit{}, nil)
	if b == nil {
		t.Fatal("expected a Beater")
	}
	if cl.seeded != "ast_initial0001" {
		t.Errorf("client token not seeded from state: %q", cl.seeded)
	}
}

func TestNewNotEnrolled(t *testing.T) {
	cases := map[string]func(*fakeState){
		"missing device_id":     func(s *fakeState) { delete(s.plain, state.KeyDeviceID) },
		"missing certificate":   func(s *fakeState) { delete(s.plain, state.KeyCertificate) },
		"missing session_token": func(s *fakeState) { delete(s.secret, state.SecretSessionToken) },
		"device_id only": func(s *fakeState) {
			delete(s.plain, state.KeyCertificate)
			delete(s.secret, state.SecretSessionToken)
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			st := newFakeState(true)
			mut(st)
			_, err := heartbeat.New(heartbeat.Deps{
				Config: &config.Config{}, Client: &fakeClient{}, State: st, Audit: &fakeAudit{},
			})
			if !errors.Is(err, heartbeat.ErrNotEnrolled) {
				t.Errorf("err = %v, want ErrNotEnrolled", err)
			}
		})
	}
}

func TestNewNilDeps(t *testing.T) {
	if _, err := heartbeat.New(heartbeat.Deps{}); err == nil {
		t.Error("New(nil config) should error")
	}
}

// ---- beat success + request construction ------------------------------------

func TestBeatSuccess(t *testing.T) {
	cl := &fakeClient{fn: always(okResp(120), nil)}
	b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := b.Beat(ctx())
	if err != nil {
		t.Fatalf("Beat: %v", err)
	}
	if cl.lastDev != "dev_hb1" {
		t.Errorf("device id = %q", cl.lastDev)
	}
	if cl.lastReq.Status != proto.Idle {
		t.Errorf("status = %q, want idle", cl.lastReq.Status)
	}
	if cl.lastReq.AgentVersion == "" || cl.lastReq.ProtocolVersion == 0 {
		t.Errorf("missing agent/protocol version: %+v", cl.lastReq)
	}
	if res.NextInterval != 120*time.Second { // rand=0.5 -> no jitter
		t.Errorf("next = %v, want 120s", res.NextInterval)
	}
}

// ---- cadence clamping + jitter ----------------------------------------------

func TestCadenceClampAndJitter(t *testing.T) {
	tests := []struct {
		name       string
		serverSecs int
		jitterPct  *int
		rand       float64
		want       time.Duration
	}{
		{"below floor clamps to 60", 0, nil, 0.5, 60 * time.Second},
		{"above ceiling clamps to 3600", 999999, nil, 0.5, 3600 * time.Second},
		{"in range, no jitter at midpoint", 300, nil, 0.5, 300 * time.Second},
		{"low jitter edge (rand=0)", 100, nil, 0.0, 80 * time.Second},         // 100*(1-0.2)
		{"high jitter edge (rand~1)", 100, nil, 1.0, 120 * time.Second},       // 100*(1+0.2)
		{"server jitter pct override", 200, intp(10), 0.0, 180 * time.Second}, // 200*(1-0.1)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := okResp(tc.serverSecs)
			resp.PollJitterPct = tc.jitterPct
			cl := &fakeClient{fn: always(resp, nil)}
			b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, func(d *heartbeat.Deps) {
				d.Rand = func() float64 { return tc.rand }
			})
			res, err := b.Beat(ctx())
			if err != nil {
				t.Fatal(err)
			}
			if res.NextInterval != tc.want {
				t.Errorf("next = %v, want %v", res.NextInterval, tc.want)
			}
		})
	}
}

// ---- token rotation ---------------------------------------------------------

func TestTokenRotationPersisted(t *testing.T) {
	resp := okResp(120)
	tok := proto.AgentSessionToken("ast_rotated0002")
	resp.AgentSessionToken = &tok
	st := newFakeState(true)
	cl := &fakeClient{fn: always(resp, nil)}
	b := newBeater(t, st, cl, &fakeAudit{}, nil)

	res, err := b.Beat(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if !res.TokenRotated || res.DurabilityGap {
		t.Errorf("rotated=%v gap=%v, want rotated=true gap=false", res.TokenRotated, res.DurabilityGap)
	}
	if string(st.secret[state.SecretSessionToken]) != "ast_rotated0002" {
		t.Errorf("token not persisted: %q", st.secret[state.SecretSessionToken])
	}
}

func TestTokenRotationPersistFailureThenRetry(t *testing.T) {
	tok := proto.AgentSessionToken("ast_rotated0003")
	rotateResp := okResp(120)
	rotateResp.AgentSessionToken = &tok

	st := newFakeState(true)
	st.putSecretErr = errors.New("disk full") // first persist fails
	// Beat 1 rotates (persist fails); Beat 2 has no rotation (retry the pending).
	cl := &fakeClient{fn: func(call int, _ string, _ proto.HeartbeatRequest) (*proto.HeartbeatResponse, error) {
		if call == 0 {
			return rotateResp, nil
		}
		return okResp(120), nil
	}}
	b := newBeater(t, st, cl, &fakeAudit{}, nil)

	res1, err := b.Beat(ctx())
	if err != nil {
		t.Fatalf("beat1: %v", err) // a durability gap is NOT a beat failure
	}
	if !res1.TokenRotated || !res1.DurabilityGap {
		t.Errorf("beat1 rotated=%v gap=%v, want both true", res1.TokenRotated, res1.DurabilityGap)
	}

	st.putSecretErr = nil // recovery
	res2, err := b.Beat(ctx())
	if err != nil {
		t.Fatalf("beat2: %v", err)
	}
	if res2.TokenRotated {
		t.Error("beat2 should not report a new rotation")
	}
	if res2.DurabilityGap {
		t.Error("beat2 should have persisted the pending token (gap closed)")
	}
	if string(st.secret[state.SecretSessionToken]) != "ast_rotated0003" {
		t.Errorf("pending token not retried/persisted: %q", st.secret[state.SecretSessionToken])
	}
}

// ---- error handling ---------------------------------------------------------

func TestBeat401AuditsAndStops(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{fn: always(nil, saasclient.ErrUnauthorized)}
	b := newBeater(t, newFakeState(true), cl, au, nil)
	_, err := b.Beat(ctx())
	if !errors.Is(err, heartbeat.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if got := au.types(); len(got) != 1 || got[0] != audit.EventAuthFailure {
		t.Errorf("audit = %v, want [auth.failure]", got)
	}
}

func TestBeat426Upgrade(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{fn: always(nil, saasclient.ErrUpgradeRequired)}
	b := newBeater(t, newFakeState(true), cl, au, nil)
	_, err := b.Beat(ctx())
	if !errors.Is(err, heartbeat.ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
	if len(au.events) != 0 {
		t.Errorf("426 must not audit, got %v", au.types())
	}
}

func TestBeatTransientFailure(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{fn: always(nil, saasclient.ErrUnexpectedStatus)}
	b := newBeater(t, newFakeState(true), cl, au, nil)
	_, err := b.Beat(ctx())
	if !errors.Is(err, heartbeat.ErrBeatFailed) {
		t.Errorf("err = %v, want ErrBeatFailed", err)
	}
	if len(au.events) != 0 {
		t.Errorf("transient failure must not audit, got %v", au.types())
	}
}

func TestBeatEmptyResponseCountsAsFailure(t *testing.T) {
	cl := &fakeClient{fn: always(nil, nil)} // (nil, nil): defensive transient path
	b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, nil)
	_, err := b.Beat(ctx())
	if !errors.Is(err, heartbeat.ErrBeatFailed) {
		t.Errorf("err = %v, want ErrBeatFailed", err)
	}
	if got := b.Stats().ConsecutiveFailures; got != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1 (empty-response path must count)", got)
	}
}

// ---- run loop ---------------------------------------------------------------

func TestRunStopsOnTerminal(t *testing.T) {
	cl := &fakeClient{fn: always(nil, saasclient.ErrUnauthorized)}
	au := &fakeAudit{}
	b := newBeater(t, newFakeState(true), cl, au, nil)
	err := b.Run(ctx())
	if !errors.Is(err, heartbeat.ErrUnauthorized) {
		t.Errorf("Run err = %v, want ErrUnauthorized", err)
	}
	if cl.calls != 1 {
		t.Errorf("calls = %d, want 1 (stop on first 401)", cl.calls)
	}
}

func TestRunContinuesOnTransientUntilCancel(t *testing.T) {
	cl := &fakeClient{fn: always(nil, saasclient.ErrUnexpectedStatus)} // always transient
	c, cancel := context.WithCancel(context.Background())
	sleeps := 0
	b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, func(d *heartbeat.Deps) {
		d.Sleep = func(sc context.Context, _ time.Duration) error {
			sleeps++
			if sleeps > 3 { // allow 3 beats, then cancel before the 4th
				cancel()
			}
			select {
			case <-sc.Done():
				return sc.Err()
			default:
				return nil
			}
		}
	})
	err := b.Run(c)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run err = %v, want context.Canceled", err)
	}
	if cl.calls != 3 {
		t.Errorf("calls = %d, want 3 transient beats before cancel (loop did not stop on transient)", cl.calls)
	}
	if got := b.Stats().ConsecutiveFailures; got != 3 {
		t.Errorf("consecutive failures = %d, want 3", got)
	}
}

// A beat whose response sets work_available=true signals the WorkAvailable channel
// (non-blocking) — the composition root forwards this to the task-poll loop.
func TestRunWorkAvailableSignalsChannel(t *testing.T) {
	resp := okResp(120)
	resp.WorkAvailable = true
	wa := make(chan struct{}, 1)
	c, cancel := context.WithCancel(context.Background())
	cl := &fakeClient{fn: always(resp, nil)}
	b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, func(d *heartbeat.Deps) {
		d.WorkAvailable = wa
		beats := 0
		d.Sleep = func(context.Context, time.Duration) error {
			beats++
			if beats > 1 {
				cancel()
				return context.Canceled
			}
			return nil
		}
	})
	_ = b.Run(c)
	select {
	case <-wa:
		// signal received
	default:
		t.Error("heartbeat did not signal WorkAvailable on work_available=true")
	}
}

// ---- stats ------------------------------------------------------------------

func TestStats(t *testing.T) {
	cl := &fakeClient{fn: always(okResp(120), nil)}
	b := newBeater(t, newFakeState(true), cl, &fakeAudit{}, nil)
	if _, err := b.Beat(ctx()); err != nil {
		t.Fatal(err)
	}
	s := b.Stats()
	if s.LastSuccess.IsZero() {
		t.Error("LastSuccess not set after a successful beat")
	}
	if s.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", s.ConsecutiveFailures)
	}
}

// ---- no secret leakage ------------------------------------------------------

func TestNoSecretLeakInLogs(t *testing.T) {
	var buf bytes.Buffer
	lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	resp := okResp(120)
	tok := proto.AgentSessionToken("ast_secretsession9999")
	resp.AgentSessionToken = &tok
	st := newFakeState(true) // seeded token ast_initial0001
	cl := &fakeClient{fn: always(resp, nil)}
	b := newBeater(t, st, cl, &fakeAudit{}, func(d *heartbeat.Deps) { d.Log = lg })

	if _, err := b.Beat(ctx()); err != nil {
		t.Fatal(err)
	}
	logs := buf.String()
	for _, secret := range []string{"secretsession9999", "ast_initial0001"} {
		if strings.Contains(logs, secret) {
			t.Errorf("secret %q leaked into logs: %s", secret, logs)
		}
	}
}

// ---- helpers ----------------------------------------------------------------

func intp(i int) *int { return &i }
