package security

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/internal/mocksaas"
	"github.com/beyzbackup/beyz-backup/internal/updater/app"
	"github.com/beyzbackup/beyz-backup/internal/updater/healthgate"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// The mocksaas fixtures are linux/amd64; the harness evaluates for that (the swap
// PRIMITIVE is host-build-tag selected, so the Windows CI runner still runs the
// real MoveFileEx on these same tests).
const (
	fxPlatform = "linux"
	fxArch     = "amd64"
	fxTargetOS = "linux"
	fxBaseline = "1.1.0"

	liveBinaryContent = "OLD-AGENT-BINARY-DO-NOT-REPLACE"
)

type mockClock struct{ t time.Time }

func (m *mockClock) now() time.Time { return m.t }
func (m *mockClock) sleep(_ context.Context, d time.Duration) error {
	m.t = m.t.Add(d)
	return nil
}

type recAuditor struct{ events []string }

func (a *recAuditor) Emit(eventType, _ string, _ map[string]any) {
	a.events = append(a.events, eventType)
}
func (a *recAuditor) has(ev string) bool {
	for _, e := range a.events {
		if e == ev {
			return true
		}
	}
	return false
}

// fakeService is the updater's service double (never started during a rejection).
type fakeService struct {
	stateDir     string
	clock        *mockClock
	reportHealth bool
	running      bool
	stops, start int
}

func (s *fakeService) Stop() error { s.stops++; s.running = false; return nil }
func (s *fakeService) Start() error {
	s.start++
	s.running = true
	if s.reportHealth {
		if m, err := health.ReadMarker(s.stateDir); err == nil {
			_ = health.WriteHealth(s.stateDir, health.Record{
				UpdateID: m.UpdateID, Result: health.ResultOK,
				WrittenAt: s.clock.now().UTC().Format(time.RFC3339),
			})
		}
	}
	return nil
}
func (s *fakeService) Running() (bool, error) { return s.running, nil }

// fetchChecker fetches the fixture manifest and runs the REAL manifestcheck.Evaluate
// (which runs the REAL verify). On proceed it resolves the placeholder artifact URL.
type fetchChecker struct {
	manifestURL, artifactURL string
	client                   *http.Client
	keys                     *trust.KeySet
}

func (c *fetchChecker) Check(_ context.Context, baseline manifest.Version) (*manifestcheck.Decision, error) {
	resp, err := c.client.Get(c.manifestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	dec, derr := manifestcheck.Evaluate(raw, c.keys, baseline, fxPlatform, fxArch)
	if dec != nil && dec.Proceed {
		dec.Artifact.URL = c.artifactURL
	}
	return dec, derr
}

type updaterKit struct {
	liveBinary string
	server     *mocksaas.Server
	svc        *fakeService
	au         *recAuditor
	upd        *app.Updater
}

func newUpdaterKit(t *testing.T, fixture string) *updaterKit {
	t.Helper()
	return newUpdaterKitWithKeys(t, fixture, mocksaas.TestKeySet())
}

// newUpdaterKitWithKeys is newUpdaterKit with an explicit trust anchor, so a test
// can drive an otherwise-valid fixture under a FOREIGN keyset (the unknown-key
// rejection path) without a new committed fixture or any production change.
func newUpdaterKitWithKeys(t *testing.T, fixture string, keys *trust.KeySet) *updaterKit {
	t.Helper()
	base := t.TempDir()
	stateDir := filepath.Join(base, "state")
	staging := filepath.Join(base, "staging")
	for _, d := range []string{stateDir, staging} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	liveBinary := filepath.Join(base, "beyz-backup-agent")
	if err := os.WriteFile(liveBinary, []byte(liveBinaryContent), 0o755); err != nil {
		t.Fatal(err)
	}
	liveConfig := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(liveConfig, []byte("old: config\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := mocksaas.NewServer(fixture)
	t.Cleanup(server.Close)
	clock := &mockClock{t: time.Unix(1700000000, 0).UTC()}
	svc := &fakeService{stateDir: stateDir, clock: clock}
	au := &recAuditor{}

	swapper, err := swap.New(
		swap.Layout{LiveBinary: liveBinary, LiveConfig: liveConfig, StagingDir: staging},
		swap.NewHTTPDownloader(server.Client()), 0)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := healthgate.New(stateDir, svc.Running,
		healthgate.WithClock(clock.now), healthgate.WithSleeper(clock.sleep), healthgate.WithPoll(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	bv, _ := manifest.ParseVersion(fxBaseline)
	upd, err := app.New(app.Deps{
		Store:        app.NewStateStore(stateDir),
		Check:        &fetchChecker{server.ManifestURL(), server.ArtifactURL(), server.Client(), keys},
		Swap:         swapper,
		Gate:         gate,
		Service:      svc,
		Marker:       app.ProdMarker{StateDir: stateDir},
		Audit:        au,
		BuildVersion: bv, TargetOS: fxTargetOS, Platform: fxPlatform, Arch: fxArch,
		HealthWindow: 90 * time.Second,
		Now:          clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &updaterKit{liveBinary: liveBinary, server: server, svc: svc, au: au, upd: upd}
}

func (k *updaterKit) liveContent(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(k.liveBinary)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
