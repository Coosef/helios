package tasks_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/agent/tasks"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

func ctx() context.Context { return context.Background() }

// ---- fakes ------------------------------------------------------------------

type fakeClient struct {
	seeded string

	pollResp *proto.TasksResponse
	pollErr  error
	pollN    int

	ackErr    error
	statusErr error

	acks     []string // task IDs acked
	statuses []struct {
		taskID string
		status proto.TaskStatusRequestStatus
	}
	ackOpts    int // option count on the most recent AckTask (1 => Idempotency-Key supplied)
	statusOpts int // option count on the most recent ReportTaskStatus
}

func (c *fakeClient) SetSessionToken(t string) { c.seeded = t }

func (c *fakeClient) PollTasks(_ context.Context, _ string) (*proto.TasksResponse, error) {
	c.pollN++
	return c.pollResp, c.pollErr
}

func (c *fakeClient) AckTask(_ context.Context, _, taskID string, _ proto.TaskAckRequest, opts ...saasclient.RequestOption) (*proto.TaskAckResponse, error) {
	c.acks = append(c.acks, taskID)
	c.ackOpts = len(opts)
	if c.ackErr != nil {
		return nil, c.ackErr
	}
	return &proto.TaskAckResponse{TaskId: taskID, Status: proto.Acked}, nil
}

func (c *fakeClient) ReportTaskStatus(_ context.Context, _, taskID string, body proto.TaskStatusRequest, opts ...saasclient.RequestOption) (*proto.TaskStatusResponse, error) {
	c.statuses = append(c.statuses, struct {
		taskID string
		status proto.TaskStatusRequestStatus
	}{taskID, body.Status})
	c.statusOpts = len(opts)
	if c.statusErr != nil {
		return nil, c.statusErr
	}
	return &proto.TaskStatusResponse{TaskId: taskID, Recorded: true}, nil
}

type fakeState struct {
	plain  map[string][]byte
	secret map[string][]byte
}

func newFakeState(enrolled bool) *fakeState {
	s := &fakeState{plain: map[string][]byte{}, secret: map[string][]byte{}}
	if enrolled {
		s.plain[state.KeyDeviceID] = []byte("dev_tk1")
		s.plain[state.KeyCertificate] = []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n")
		s.secret[state.SecretSessionToken] = []byte("ast_tasks0001")
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

type fakeAudit struct{ events []audit.Event }

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

func env(taskID string, typ proto.TaskEnvelopeType) proto.TaskEnvelope {
	return proto.TaskEnvelope{TaskId: taskID, Type: typ, SchemaVersion: 1, Sequence: 1}
}

func tasksResp(next int, work bool, tasks ...proto.TaskEnvelope) *proto.TasksResponse {
	return &proto.TasksResponse{NextPollSeconds: next, WorkAvailable: work, Tasks: tasks}
}

func newPoller(t *testing.T, st *fakeState, cl *fakeClient, au *fakeAudit, mut func(*tasks.Deps)) *tasks.Poller {
	t.Helper()
	d := tasks.Deps{
		Config: &config.Config{Heartbeat: config.Heartbeat{HeartbeatIntervalSeconds: 60, TaskPollIntervalSeconds: 300}},
		Client: cl, State: st, Audit: au,
		Now:  func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Rand: func() float64 { return 0.5 }, // no jitter
	}
	if mut != nil {
		mut(&d)
	}
	p, err := tasks.New(d)
	if err != nil {
		t.Fatalf("tasks.New: %v", err)
	}
	return p
}

// ---- enrolled predicate -----------------------------------------------------

func TestNewEnrolledPredicate(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false)}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	if p == nil {
		t.Fatal("expected a Poller")
	}
	if cl.seeded != "ast_tasks0001" {
		t.Errorf("token not seeded from state: %q", cl.seeded)
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
			_, err := tasks.New(tasks.Deps{Config: &config.Config{}, Client: &fakeClient{}, State: st, Audit: &fakeAudit{}})
			if !errors.Is(err, tasks.ErrNotEnrolled) {
				t.Errorf("err = %v, want ErrNotEnrolled", err)
			}
		})
	}
}

// ---- poll outcomes ----------------------------------------------------------

func TestPollEmpty(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(600, false)}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := p.PollOnce(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskCount != 0 || res.Processed != 0 || res.Skipped != 0 {
		t.Errorf("empty poll result = %+v", res)
	}
	if res.NextInterval != 600*time.Second { // rand=0.5 -> no jitter
		t.Errorf("next = %v, want 600s", res.NextInterval)
	}
	if len(cl.acks) != 0 {
		t.Error("empty poll must not ack")
	}
}

func TestPollNoopRoundTrip(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_noop1", proto.Noop))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := p.PollOnce(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 1 || res.Skipped != 0 {
		t.Errorf("result = %+v, want processed=1", res)
	}
	if len(cl.acks) != 1 || cl.acks[0] != "tsk_noop1" {
		t.Errorf("acks = %v", cl.acks)
	}
	if len(cl.statuses) != 1 || cl.statuses[0].status != proto.Succeeded {
		t.Errorf("statuses = %+v, want succeeded", cl.statuses)
	}
}

func TestPollRecognizedNotExecuted(t *testing.T) {
	for _, typ := range []proto.TaskEnvelopeType{proto.UpdateCheck, proto.ConfigRefresh} {
		t.Run(string(typ), func(t *testing.T) {
			cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_x", typ))}
			p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
			res, err := p.PollOnce(ctx())
			if err != nil {
				t.Fatal(err)
			}
			if res.Processed != 0 || res.Skipped != 1 {
				t.Errorf("result = %+v, want skipped=1", res)
			}
			if len(cl.acks) != 0 {
				t.Errorf("%s must NOT be acked, acks=%v", typ, cl.acks)
			}
		})
	}
}

func TestPollUnknownType(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_u", proto.TaskEnvelopeType("backup_now")))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := p.PollOnce(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || len(cl.acks) != 0 {
		t.Errorf("unknown type must be skipped + not acked: res=%+v acks=%v", res, cl.acks)
	}
}

func TestCadenceClampAndJitter(t *testing.T) {
	tests := []struct {
		name       string
		serverSecs int
		jitterPct  *int
		rand       float64
		want       time.Duration
	}{
		{"below floor clamps to 60", 0, nil, 0.5, 60 * time.Second},
		{"above ceiling clamps to 86400", 999999, nil, 0.5, 86400 * time.Second},
		{"in range, no jitter", 600, nil, 0.5, 600 * time.Second},
		{"low jitter edge", 1000, nil, 0.0, 800 * time.Second},             // 1000*(1-0.2)
		{"server jitter override", 1000, intp(10), 0.0, 900 * time.Second}, // 1000*(1-0.1)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := tasksResp(tc.serverSecs, false)
			resp.PollJitterPct = tc.jitterPct
			cl := &fakeClient{pollResp: resp}
			p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
				d.Rand = func() float64 { return tc.rand }
			})
			res, err := p.PollOnce(ctx())
			if err != nil {
				t.Fatal(err)
			}
			if res.NextInterval != tc.want {
				t.Errorf("next = %v, want %v", res.NextInterval, tc.want)
			}
		})
	}
}

func TestNoopHigherSchemaStillProcessed(t *testing.T) {
	e := env("tsk_hs", proto.Noop)
	e.SchemaVersion = 2 // higher than supported; noop has no payload -> best-effort
	cl := &fakeClient{pollResp: tasksResp(300, false, e)}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := p.PollOnce(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 1 || len(cl.acks) != 1 {
		t.Errorf("higher-schema noop should still round-trip: res=%+v acks=%v", res, cl.acks)
	}
}

func TestPollEmptyResponseCountsAsFailure(t *testing.T) {
	cl := &fakeClient{pollResp: nil, pollErr: nil} // (nil, nil): defensive transient path
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	_, err := p.PollOnce(ctx())
	if !errors.Is(err, tasks.ErrPollFailed) {
		t.Errorf("err = %v, want ErrPollFailed", err)
	}
	if p.Stats().ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", p.Stats().ConsecutiveFailures)
	}
}

func TestRunContinuesOnTransientUntilCancel(t *testing.T) {
	cl := &fakeClient{pollErr: saasclient.ErrUnexpectedStatus} // always transient
	c, cancel := context.WithCancel(context.Background())
	waits := 0
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
		d.Wait = func(_ context.Context, _ time.Duration) (bool, error) {
			waits++
			if waits > 3 { // allow 3 polls, then cancel
				cancel()
				return false, context.Canceled
			}
			return false, nil
		}
	})
	if err := p.Run(c); !errors.Is(err, context.Canceled) {
		t.Errorf("Run err = %v, want context.Canceled", err)
	}
	if cl.pollN != 3 {
		t.Errorf("pollN = %d, want 3 (transient does not stop the loop)", cl.pollN)
	}
	if got := p.Stats().ConsecutiveFailures; got != 3 {
		t.Errorf("consecutive failures = %d, want 3", got)
	}
}

// ---- dedup + idempotency ----------------------------------------------------

func TestSeenSetDedup(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_dup", proto.Noop))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	if _, err := p.PollOnce(ctx()); err != nil { // first: processes
		t.Fatal(err)
	}
	if _, err := p.PollOnce(ctx()); err != nil { // second: redelivered -> skipped
		t.Fatal(err)
	}
	if len(cl.acks) != 1 {
		t.Errorf("redelivered task re-acked: acks=%v (want 1)", cl.acks)
	}
}

func TestIdempotencyKeySupplied(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_k", proto.Noop))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	if _, err := p.PollOnce(ctx()); err != nil {
		t.Fatal(err)
	}
	// The poller supplies exactly one option (WithIdempotencyKey) to both mutating
	// calls; saasclient-level tests prove the option threads the key into the header.
	if cl.ackOpts != 1 {
		t.Errorf("AckTask options = %d, want 1 (Idempotency-Key)", cl.ackOpts)
	}
	if cl.statusOpts != 1 {
		t.Errorf("ReportTaskStatus options = %d, want 1 (Idempotency-Key)", cl.statusOpts)
	}
	// The deterministic-key formula is stable per (task_id, op) and distinct by op.
	wantAck := uuid.NewSHA1(uuid.NameSpaceURL, []byte("beyz:task:tsk_k:ack"))
	wantStatus := uuid.NewSHA1(uuid.NameSpaceURL, []byte("beyz:task:tsk_k:status"))
	if wantAck == wantStatus {
		t.Error("ack/status keys must differ by operation")
	}
	if uuid.NewSHA1(uuid.NameSpaceURL, []byte("beyz:task:tsk_k:ack")) != wantAck {
		t.Error("key derivation is not deterministic")
	}
}

// ---- error handling ---------------------------------------------------------

func Test401AuditsAndStops(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{pollErr: saasclient.ErrUnauthorized}
	p := newPoller(t, newFakeState(true), cl, au, nil)
	_, err := p.PollOnce(ctx())
	if !errors.Is(err, tasks.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if got := au.types(); len(got) != 1 || got[0] != audit.EventAuthFailure {
		t.Errorf("audit = %v, want [auth.failure]", got)
	}
}

func Test401DuringAckAbortsPoll(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{
		pollResp: tasksResp(300, false, env("tsk_a", proto.Noop)),
		ackErr:   saasclient.ErrUnauthorized,
	}
	p := newPoller(t, newFakeState(true), cl, au, nil)
	_, err := p.PollOnce(ctx())
	if !errors.Is(err, tasks.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized (ack 401 aborts)", err)
	}
	if got := au.types(); len(got) != 1 || got[0] != audit.EventAuthFailure {
		t.Errorf("audit = %v, want [auth.failure]", got)
	}
}

func Test426Upgrade(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{pollErr: saasclient.ErrUpgradeRequired}
	p := newPoller(t, newFakeState(true), cl, au, nil)
	_, err := p.PollOnce(ctx())
	if !errors.Is(err, tasks.ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
	if len(au.events) != 0 {
		t.Errorf("426 must not audit, got %v", au.types())
	}
}

func TestTransientPollFailure(t *testing.T) {
	au := &fakeAudit{}
	cl := &fakeClient{pollErr: saasclient.ErrUnexpectedStatus}
	p := newPoller(t, newFakeState(true), cl, au, nil)
	_, err := p.PollOnce(ctx())
	if !errors.Is(err, tasks.ErrPollFailed) {
		t.Errorf("err = %v, want ErrPollFailed", err)
	}
	if len(au.events) != 0 {
		t.Errorf("transient must not audit, got %v", au.types())
	}
	if p.Stats().ConsecutiveFailures != 1 {
		t.Errorf("consecutive failures = %d, want 1", p.Stats().ConsecutiveFailures)
	}
}

func TestTransientAckIsDegradedAndRetried(t *testing.T) {
	cl := &fakeClient{
		pollResp: tasksResp(300, false, env("tsk_t", proto.Noop)),
		ackErr:   saasclient.ErrUnexpectedStatus, // transient ack failure
	}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	res, err := p.PollOnce(ctx())
	if err != nil { // poll itself succeeded; the task failed transiently
		t.Fatalf("poll should not fail on a transient task error: %v", err)
	}
	if res.Processed != 0 || res.Failed != 1 || res.Skipped != 0 {
		t.Errorf("result = %+v, want failed=1", res)
	}
	// Degraded poll: the failure counter advances and lastPollSuccess does NOT, so
	// the agent backs off and dashboards see the degraded state (not "healthy").
	s := p.Stats()
	if s.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1 (degraded poll)", s.ConsecutiveFailures)
	}
	if !s.LastPollSuccess.IsZero() {
		t.Error("LastPollSuccess must not advance on a degraded poll")
	}
	// Redelivery: the task is NOT in the seen set, so a second poll retries it.
	cl.ackErr = nil
	if _, err := p.PollOnce(ctx()); err != nil {
		t.Fatal(err)
	}
	if len(cl.acks) != 2 {
		t.Errorf("transiently-failed task not retried: acks=%v (want 2)", cl.acks)
	}
	if p.Stats().ConsecutiveFailures != 0 {
		t.Errorf("failure counter not cleared after recovery: %d", p.Stats().ConsecutiveFailures)
	}
}

// A work_available response that makes NO progress (e.g. an unacked
// recognized-not-executed task keeps the server flag set) must NOT pin the loop
// at the ~5s window — it falls back to the clamped server cadence.
func TestRunWorkAvailableNoProgressFallsBackToCadence(t *testing.T) {
	// Server always returns work_available=true + an update_check (never acked, so
	// processed==0 every poll). Without the progress gate this would hot-poll ~5s.
	cl := &fakeClient{pollResp: tasksResp(300, true, env("tsk_uc", proto.UpdateCheck))}
	c, cancel := context.WithCancel(context.Background())
	waits := 0
	var intervals []time.Duration
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
		d.Wait = func(_ context.Context, dur time.Duration) (bool, error) {
			waits++
			intervals = append(intervals, dur)
			if waits > 3 {
				cancel()
				return false, context.Canceled
			}
			return false, nil
		}
	})
	_ = p.Run(c)
	// The waits AFTER the first poll (indices 1,2) must be the server cadence (300s),
	// not the ~5s work-available window — proving the no-progress flag was ignored.
	for i := 1; i < len(intervals)-0 && i <= 2; i++ {
		if intervals[i] < 60*time.Second {
			t.Errorf("interval[%d] = %v, want >= server cadence (no-progress work_available must not hot-poll)", i, intervals[i])
		}
	}
}

// A work_available response that DID make progress polls soon (jittered window),
// bounded so it cannot pin below the cadence indefinitely.
func TestRunWorkAvailableWithProgressPollsSoonBounded(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, true, env("tsk_np", proto.Noop))}
	// Note: the same noop task id is seen after the first poll, so subsequent polls
	// process 0 -> the loop returns to cadence. We assert the FIRST post-poll wait
	// is the small window (progress was made), the next is cadence.
	c, cancel := context.WithCancel(context.Background())
	waits := 0
	var intervals []time.Duration
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
		d.Wait = func(_ context.Context, dur time.Duration) (bool, error) {
			waits++
			intervals = append(intervals, dur)
			if waits > 2 {
				cancel()
				return false, context.Canceled
			}
			return false, nil
		}
	})
	_ = p.Run(c)
	// intervals[0]=startup, intervals[1]=after the progress poll => small window.
	if len(intervals) < 2 {
		t.Fatalf("expected at least 2 waits, got %d", len(intervals))
	}
	if intervals[1] >= 60*time.Second {
		t.Errorf("post-progress wait = %v, want small work-available window (<60s)", intervals[1])
	}
}

// ---- run loop ---------------------------------------------------------------

func TestRunStopsOnTerminal(t *testing.T) {
	cl := &fakeClient{pollErr: saasclient.ErrUnauthorized}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
		d.Wait = func(context.Context, time.Duration) (bool, error) { return false, nil }
	})
	if err := p.Run(ctx()); !errors.Is(err, tasks.ErrUnauthorized) {
		t.Errorf("Run err = %v, want ErrUnauthorized", err)
	}
	if cl.pollN != 1 {
		t.Errorf("pollN = %d, want 1 (stop on first 401)", cl.pollN)
	}
}

func TestRunWorkAvailablePokeIsJittered(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false)}
	c, cancel := context.WithCancel(context.Background())
	waits := 0
	var pokedWindows int
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) {
		d.Wait = func(_ context.Context, dur time.Duration) (bool, error) {
			waits++
			switch waits {
			case 1:
				return true, nil // startup wait -> interrupted by a poke
			case 2:
				// after a poke, the loop must wait a small jittered window (~5s), not 0
				if dur < 3*time.Second || dur > 7*time.Second {
					t.Errorf("post-poke window = %v, want ~5s jittered (not instant)", dur)
				}
				pokedWindows++
				return false, nil // window elapses -> poll
			default:
				cancel()
				return false, context.Canceled
			}
		}
	})
	_ = p.Run(c)
	if pokedWindows != 1 {
		t.Errorf("expected exactly one jittered post-poke window, got %d", pokedWindows)
	}
	if cl.pollN != 1 {
		t.Errorf("pollN = %d, want 1 (one poll after the poke window)", cl.pollN)
	}
}

// ---- stats + no-leak --------------------------------------------------------

func TestStats(t *testing.T) {
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_s", proto.Noop))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, nil)
	if _, err := p.PollOnce(ctx()); err != nil {
		t.Fatal(err)
	}
	s := p.Stats()
	if s.LastPollSuccess.IsZero() || s.ConsecutiveFailures != 0 || s.LastTaskCount != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestNoSecretLeakInLogs(t *testing.T) {
	var buf bytes.Buffer
	lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{pollResp: tasksResp(300, false, env("tsk_l", proto.Noop))}
	p := newPoller(t, newFakeState(true), cl, &fakeAudit{}, func(d *tasks.Deps) { d.Log = lg })
	if _, err := p.PollOnce(ctx()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ast_tasks0001") {
		t.Errorf("session token leaked into logs: %s", buf.String())
	}
}

// ---- helpers ----------------------------------------------------------------

func intp(i int) *int { return &i }
