package integration

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/app"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
)

// These tests exercise the one-shot enrollment token FILE end-to-end through the
// REAL composition root (app.New + app.Run): the installer-dropped file is the
// only token source (no env), and the real enroll exchange against the fake SaaS
// drives the consume/delete lifecycle. The unit tests in internal/agent/app cover
// the branch matrix; here we prove it holds with real HTTP + real state.

// newOneShotApp builds a real App whose ONLY possible token source is the one-shot
// file in stateDir (the config carries no enrollment token).
func newOneShotApp(t *testing.T, srv *httptest.Server, pin, stateDir string) *app.App {
	t.Helper()
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		General: config.General{
			APIBaseURL: srv.URL, TenantID: "tnt_int", Region: "eu-central-1",
			// EnrollmentToken intentionally empty: the file is the only source.
		},
		Heartbeat: config.Heartbeat{HeartbeatIntervalSeconds: 60, TaskPollIntervalSeconds: 300},
		Logging:   config.Logging{Level: "info", Format: "json", FilePath: filepath.Join(t.TempDir(), "agent.log")},
	}
	a, err := app.New(app.Options{Config: cfg, StateDir: stateDir, BootstrapPins: []string{pin}, Protector: prot})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func writeOneShotToken(t *testing.T, stateDir, token string) string {
	t.Helper()
	path := filepath.Join(stateDir, "enroll-token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write one-shot token: %v", err)
	}
	return path
}

func waitGone(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s still present after %s (expected purge)", path, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 201 path: the file token enrolls against the fake SaaS and is deleted on the
// successful (definitive) outcome.
func TestIntegrationOneShotTokenEnrollsAndDeletes(t *testing.T) {
	f, srv, pin := newFakeSaaS(t) // enrollStatus 201 (default)
	stateDir := t.TempDir()
	const token = "bzt_oneshot_aaaa1111bbbb2222cccc3333"
	tokenPath := writeOneShotToken(t, stateDir, token)

	a := newOneShotApp(t, srv, pin, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// The file is purged only on a definitive outcome; with 201 that is success.
	waitGone(t, tokenPath, 3*time.Second)

	// The real enroll request must have carried the FILE token (in the body only).
	req, ok := f.lastTo("/v1/enroll")
	if !ok {
		t.Fatal("no enroll request reached the fake SaaS")
	}
	if !strings.Contains(req.Body, token) {
		t.Fatal("enroll request body did not carry the one-shot file token")
	}
	if strings.Contains(req.RawQuery, token) || strings.Contains(strings.Join(req.Header["Authorization"], ""), token) {
		t.Fatal("token leaked into the URL/query or an Authorization header")
	}

	cancel()
	select {
	case err := <-done:
		// Cancel during the (sleeping) heartbeat loop is a graceful shutdown.
		if err != nil {
			t.Fatalf("Run after cancel = %v, want nil (graceful)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// 409 path: a consumed/rejected token is deleted AND the outcome is the terminal
// re-enrollment-required signal (no heartbeat loop is entered).
func TestIntegrationOneShotToken409DeletesAndTerminal(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	f.enrollStatus = 409 // server reports the token already consumed
	stateDir := t.TempDir()
	tokenPath := writeOneShotToken(t, stateDir, "bzt_oneshot_dead_dddd4444eeee5555")

	a := newOneShotApp(t, srv, pin, stateDir)

	// 409 is definitive and terminal, so Run returns synchronously (no loops).
	err := a.Run(context.Background())
	if !errors.Is(err, app.ErrEnrollmentRequired) {
		t.Fatalf("Run = %v, want ErrEnrollmentRequired", err)
	}
	if _, statErr := os.Stat(tokenPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("rejected one-shot token file was not deleted")
	}
	if _, ok := f.lastTo("/v1/enroll"); !ok {
		t.Fatal("no enroll request reached the fake SaaS")
	}
}
