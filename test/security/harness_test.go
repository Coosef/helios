// Package security holds the S1-T32 "theme-G" negative security suite. These
// tests RE-EXERCISE the security controls end-to-end (a real SPKI-pinned client
// against an in-process TLS control-plane, the real enroll/heartbeat use-cases
// writing real log files, the real updater FSM over signed mocksaas fixtures, and
// the real state protector) and assert the FAIL-CLOSED outcome: bad input is
// refused, secrets never reach a log, and nothing is staged/swapped on rejection.
//
// They are test-only, run under `go test ./...` (so the required CI `test` job
// covers them) and under the named `task test:negative` -> the blocking CI
// `security` job. No external network: httptest + mocksaas + fakeSaaS only.
package security

import (
	"context"
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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/heartbeat"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
)

// The KNOWN secrets that must NEVER appear in any log stream.
const (
	enrollToken  = "bzt_neg_secret_aaaa1111bbbb2222cccc3333"
	sessionToken = "ast_neg_secret_session_4444dddd5555eeee"
)

// ---- controllable in-process TLS control-plane (exercises the real client) ----

type capturedReq struct {
	Method, Path, RawQuery, Body string
	Header                       http.Header
}

type fakeSaaS struct {
	mu              sync.Mutex
	reqs            []capturedReq
	enrollStatus    int    // 201 default; 409 for token-replay / clone-conflict
	enrollProblem   string // problem title on a non-201 enroll (e.g. clone)
	heartbeatStatus int    // 200 default
	certPEM         string
}

func newFakeSaaS(t *testing.T) (*fakeSaaS, *httptest.Server, string) {
	t.Helper()
	f := &fakeSaaS{enrollStatus: 201, heartbeatStatus: 200, certPEM: selfSignedCertPEM(t)}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return f, srv, httpclient.PinFromCertificate(srv.Certificate())
}

func (f *fakeSaaS) set(fn func(*fakeSaaS)) { f.mu.Lock(); defer f.mu.Unlock(); fn(f) }

func (f *fakeSaaS) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, capturedReq{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.Header.Clone()})
}

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
	es, hs, prob, cert := f.enrollStatus, f.heartbeatStatus, f.enrollProblem, f.certPEM
	f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/enroll"):
		if es != 201 {
			title := prob
			if title == "" {
				title = "token already consumed"
			}
			writeProblem(w, es, title)
			return
		}
		writeJSON(w, 201, fmt.Sprintf(
			`{"device_id":"dev_neg_1","tenant_id":"tnt_int","region":"eu-central-1",`+
				`"agent_certificate_pem":%q,"agent_session_token":%q,`+
				`"recovery_policy":"escrowed","cert_not_after":"2026-07-08T00:00:00Z"}`, cert, sessionToken))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/heartbeat"):
		if hs != 200 {
			writeProblem(w, hs, "heartbeat error")
			return
		}
		writeJSON(w, 200, `{"next_heartbeat_seconds":60,"next_task_poll_seconds":120,"server_time":"2026-06-15T00:00:00Z"}`)
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

func selfSignedCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "dev_neg_1"},
		NotBefore: time.Unix(1700000000, 0), NotAfter: time.Unix(1800000000, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// ---- agent kit: a REAL enroll/heartbeat pipeline writing real log files ----

type agentKit struct {
	cfg         *config.Config
	store       *state.Store
	client      *saasclient.Client
	log         *logging.Logger
	em          *audit.Emitter
	agentLog    string
	securityLog string
}

// newAgentKitPinned wires the agent against srv using `pin` (pass a wrong pin to
// exercise the SPKI mismatch path).
func newAgentKitPinned(t *testing.T, srv *httptest.Server, pin string) *agentKit {
	t.Helper()
	tmp := t.TempDir()
	agentLog := filepath.Join(tmp, "agent.log")
	securityLog := filepath.Join(tmp, "security.log")

	log, err := logging.New(logging.Options{Level: "info", Format: "json", FilePath: agentLog, Component: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	em, err := audit.New(audit.NewFileAppender(securityLog), nil, audit.Identity{Source: audit.SourceAgent, DeviceGUID: "g-neg"})
	if err != nil {
		t.Fatal(err)
	}

	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(state.Options{Dir: filepath.Join(tmp, "state"), Protector: prot})
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

	cfg := &config.Config{
		General: config.General{
			APIBaseURL: srv.URL, TenantID: "tnt_int", Region: "eu-central-1",
			EnrollmentToken: config.Secret(enrollToken),
		},
		Heartbeat: config.Heartbeat{HeartbeatIntervalSeconds: 60, TaskPollIntervalSeconds: 300},
	}
	return &agentKit{cfg, store, client, log, em, agentLog, securityLog}
}

// newAgentKit wires the agent with the CORRECT pin (the happy path).
func newAgentKit(t *testing.T, srv *httptest.Server, pin string) *agentKit {
	return newAgentKitPinned(t, srv, pin)
}

func (k *agentKit) enroller(t *testing.T) *enroll.Enroller {
	t.Helper()
	idm, err := identity.New(k.store)
	if err != nil {
		t.Fatal(err)
	}
	e, err := enroll.New(enroll.Deps{Config: k.cfg, Identity: idm, Client: k.client, State: k.store, Audit: k.em, Log: k.log})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (k *agentKit) beater(t *testing.T) *heartbeat.Beater {
	t.Helper()
	b, err := heartbeat.New(heartbeat.Deps{
		Config: k.cfg, Client: k.client, State: k.store, Audit: k.em, Log: k.log,
		Now:   func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Sleep: func(context.Context, time.Duration) error { return nil },
		Rand:  func() float64 { return 0.5 },
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seedSessionToken mirrors what the composition root does after enrollment: the
// persisted bearer is loaded into the client so the heartbeat is authenticated
// (so the bearer is actually SENT — making the secret-leak grep meaningful).
func (k *agentKit) seedSessionToken(t *testing.T) {
	t.Helper()
	tok, err := k.store.GetSecret(state.SecretSessionToken)
	if err != nil {
		t.Fatalf("read session token: %v", err)
	}
	k.client.SetSessionToken(string(tok))
}

// flushLogs closes the logger so the file sink is fully written before grepping
// (the audit FileAppender already writes each record synchronously).
func (k *agentKit) flushLogs(t *testing.T) {
	t.Helper()
	if err := k.log.Close(); err != nil {
		t.Errorf("close logger: %v", err)
	}
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}
