// Package integration holds S1-T34 end-to-end tests that wire REAL components
// (agent use-cases + updater FSM) against in-process servers. They are test-only
// (every file is _test.go), deterministic (httptest + injected clocks + t.TempDir),
// and source-portable (the same tests run on Linux/macOS and the Windows CI runner,
// where the updater suite executes the real MoveFileEx swap).
package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// capturedReq is one request the fake SaaS received (for assertions).
type capturedReq struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     string
}

// fakeSaaS is a controllable, in-process TLS control-plane mock. Unlike the Prism
// contract mock, it serves over HTTPS so the agent's mandatory SPKI pinning is
// satisfied (pin = the httptest cert's SPKI). Knobs let a test force 409/401/426/5xx
// and inject reserved/unknown response fields (forward-compat).
type fakeSaaS struct {
	mu   sync.Mutex
	reqs []capturedReq

	enrollStatus         int    // 201 default; 409 to simulate a consumed token
	heartbeatStatus      int    // 200 default; 401/426/500 to drive error paths
	tasksStatus          int    // 200 default
	nextHeartbeatSeconds int    // cadence echoed in the heartbeat response
	nextPollSeconds      int    // cadence echoed in the tasks response
	extraHeartbeatJSON   string // reserved/unknown fields spliced into the heartbeat body
	extraTasksJSON       string // reserved/unknown fields spliced into the tasks body
	certPEM              string // agent_certificate_pem returned on enroll/register
}

func newFakeSaaS(t *testing.T) (*fakeSaaS, *httptest.Server, string) {
	t.Helper()
	f := &fakeSaaS{
		enrollStatus: 201, heartbeatStatus: 200, tasksStatus: 200,
		nextHeartbeatSeconds: 60, nextPollSeconds: 120, certPEM: selfSignedCertPEM(t),
	}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return f, srv, httpclient.PinFromCertificate(srv.Certificate())
}

func (f *fakeSaaS) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, capturedReq{
		Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery,
		Header: r.Header.Clone(), Body: string(body),
	})
}

// requests returns a copy of the captured requests.
func (f *fakeSaaS) requests() []capturedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedReq, len(f.reqs))
	copy(out, f.reqs)
	return out
}

func (f *fakeSaaS) lastTo(suffix string) (capturedReq, bool) {
	rs := f.requests()
	for i := len(rs) - 1; i >= 0; i-- {
		if strings.HasSuffix(rs[i].Path, suffix) {
			return rs[i], true
		}
	}
	return capturedReq{}, false
}

func (f *fakeSaaS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	f.mu.Lock()
	es, hs, ts := f.enrollStatus, f.heartbeatStatus, f.tasksStatus
	nh, np := f.nextHeartbeatSeconds, f.nextPollSeconds
	eh, et, cert := f.extraHeartbeatJSON, f.extraTasksJSON, f.certPEM
	f.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/enroll"):
		if es != 201 {
			writeProblem(w, es, "token already consumed")
			return
		}
		writeJSON(w, 201, fmt.Sprintf(
			`{"device_id":"dev_int_1","tenant_id":"tnt_int","region":"eu-central-1",`+
				`"agent_certificate_pem":%q,"agent_session_token":"ast_enroll_1",`+
				`"recovery_policy":"escrowed","cert_not_after":"2026-07-08T00:00:00Z"}`, cert))

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/register"):
		writeJSON(w, 200, fmt.Sprintf(
			`{"device_id":"dev_int_1","agent_certificate_pem":%q,"agent_session_token":"ast_reg_2"}`, cert))

	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/heartbeat"):
		if hs != 200 {
			writeProblem(w, hs, "heartbeat error")
			return
		}
		writeJSON(w, 200, fmt.Sprintf(
			`{"next_heartbeat_seconds":%d,"next_task_poll_seconds":%d,"server_time":"2026-06-12T00:00:00Z"%s}`,
			nh, np, eh))

	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tasks"):
		if ts != 200 {
			writeProblem(w, ts, "tasks error")
			return
		}
		writeJSON(w, 200, fmt.Sprintf(
			`{"tasks":[],"work_available":false,"next_poll_seconds":%d,"server_time":"2026-06-12T00:00:00Z"%s}`,
			np, et))

	default:
		writeProblem(w, http.StatusNotFound, "no route")
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d}`, detail, status))
}

// selfSignedCertPEM returns a real, parseable cert PEM (defensive: enroll stores it
// opaquely today, but a realistic cert avoids future surprises if anything parses it).
func selfSignedCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dev_int_1"},
		NotBefore:    time.Unix(1700000000, 0),
		NotAfter:     time.Unix(1800000000, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// ---- proto request helpers --------------------------------------------------

func protoRegisterReq() proto.RegisterRequest   { return proto.RegisterRequest{} }
func protoHeartbeatReq() proto.HeartbeatRequest { return proto.HeartbeatRequest{} }
