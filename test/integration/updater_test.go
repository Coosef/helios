package integration

import (
	"context"
	"errors"
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
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

func swapperFor(liveBinary, liveConfig, staging string, server *mocksaas.Server) (*swap.Swapper, error) {
	return swap.New(
		swap.Layout{LiveBinary: liveBinary, LiveConfig: liveConfig, StagingDir: staging},
		swap.NewHTTPDownloader(server.Client()), 0)
}

// stageBackupSwap drives the REAL swap primitives to a swapped on-disk state
// (live = new artifact, .bak = old, digest recorded) — the setup for the AC-28
// crash-recovery resume test.
func stageBackupSwap(t *testing.T, k *updaterKit) {
	t.Helper()
	art, ok := k.server.Artifact(fxPlatform, fxArch)
	if !ok {
		t.Fatal("no artifact for the valid fixture")
	}
	if err := k.swapper.Stage(context.Background(), art, fxTargetOS); err != nil {
		t.Fatal(err)
	}
	dg, err := k.swapper.Backup()
	if err != nil {
		t.Fatal(err)
	}
	k.backupDigest = dg.String()
	if err := k.swapper.Swap(); err != nil {
		t.Fatal(err)
	}
}

// The fixtures are linux/amd64; the harness always evaluates for that (the swap
// PRIMITIVE — MoveFileEx vs rename — is selected by the HOST build tag, so the
// Windows CI runner still executes the real MoveFileEx on these same tests).
const (
	fxPlatform = "linux"
	fxArch     = "amd64"
	fxTargetOS = "linux"
	fxBaseline = "1.1.0"
)

// mockClock is shared by the Updater, the health gate, and the health-report
// simulator so the 90s window is deterministic (no real sleeps).
type mockClock struct{ t time.Time }

func (m *mockClock) now() time.Time { return m.t }
func (m *mockClock) sleep(_ context.Context, d time.Duration) error {
	m.t = m.t.Add(d)
	return nil
}

// recAuditor records emitted updater events for assertions.
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

// fakeService is the updater's service double. On Start it OPTIONALLY simulates the
// new agent self-reporting by writing health.json (echoing the marker's update_id),
// which is exactly what makes the real health gate pass.
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

// fetchChecker fetches the fixture manifest from the mocksaas server and runs the
// REAL manifestcheck.Evaluate (which runs the REAL verify). On proceed it resolves
// the manifest's https placeholder artifact URL to the live server.
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
		dec.Artifact.URL = c.artifactURL // resolve placeholder -> live server
	}
	return dec, derr
}

type updaterKit struct {
	stateDir, liveBinary string
	server               *mocksaas.Server
	svc                  *fakeService
	clock                *mockClock
	au                   *recAuditor
	swapper              *swap.Swapper
	backupDigest         string
	upd                  *app.Updater
}

func newUpdaterKit(t *testing.T, fixture string, reportHealth bool) *updaterKit {
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
	if err := os.WriteFile(liveBinary, []byte("OLD-AGENT-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	liveConfig := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(liveConfig, []byte("old: config\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := mocksaas.NewServer(fixture)
	t.Cleanup(server.Close)
	clock := &mockClock{t: time.Unix(1700000000, 0).UTC()}
	svc := &fakeService{stateDir: stateDir, clock: clock, reportHealth: reportHealth}
	au := &recAuditor{}

	swapper, err := swapperFor(liveBinary, liveConfig, staging, server)
	if err != nil {
		t.Fatal(err)
	}
	kit := &updaterKit{stateDir: stateDir, liveBinary: liveBinary, server: server, svc: svc, clock: clock, au: au, swapper: swapper}
	gate, err := healthgate.New(stateDir, svc.Running,
		healthgate.WithClock(clock.now), healthgate.WithSleeper(clock.sleep), healthgate.WithPoll(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	bv, _ := manifest.ParseVersion(fxBaseline)
	upd, err := app.New(app.Deps{
		Store:        app.NewStateStore(stateDir),
		Check:        &fetchChecker{server.ManifestURL(), server.ArtifactURL(), server.Client(), mocksaas.TestKeySet()},
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
	kit.upd = upd
	return kit
}

func (k *updaterKit) liveContent(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(k.liveBinary)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- AC-26: valid fixture applies and commits -------------------------------

func TestIntegrationUpdaterValidApplies(t *testing.T) {
	k := newUpdaterKit(t, "valid", true) // agent self-reports healthy
	out, err := k.upd.Apply(context.Background())
	if out != app.OutcomeUpdated || err != nil {
		t.Fatalf("apply = %s, %v; want updated", out, err)
	}
	// the live binary is now the new artifact bytes (real swap occurred)
	if k.liveContent(t) != string(mocksaas.ByName("valid").Artifact) {
		t.Error("live binary was not replaced with the new artifact")
	}
	for _, ev := range []string{"update.staged", "update.swapped", "update.health_ok", "update.succeeded"} {
		if !k.au.has(ev) {
			t.Errorf("missing audit event %s (events=%v)", ev, k.au.events)
		}
	}
	if k.svc.stops == 0 || k.svc.start == 0 {
		t.Error("service was not stopped+started around the swap (AC-26)")
	}
}

// ---- AC-29: invalid signature never swaps -----------------------------------

func TestIntegrationUpdaterInvalidSignatureNeverSwaps(t *testing.T) {
	k := newUpdaterKit(t, "invalid-signature", true)
	out, err := k.upd.Apply(context.Background())
	if out != app.OutcomeRejected {
		t.Errorf("out = %s, want rejected", out)
	}
	// Rejected for the RIGHT reason: the Ed25519 signature did not verify.
	if !errors.Is(err, verify.ErrSignatureInvalid) {
		t.Errorf("err = %v, want verify.ErrSignatureInvalid", err)
	}
	if k.liveContent(t) != "OLD-AGENT-BINARY" {
		t.Error("live binary changed despite a bad signature (AC-29)")
	}
	if k.svc.stops != 0 {
		t.Error("a rejected manifest must not even stop the agent")
	}
	if !k.au.has("update.signature_invalid") {
		t.Errorf("missing update.signature_invalid (events=%v)", k.au.events)
	}
}

// ---- AC-24: hash mismatch aborts before swap --------------------------------

func TestIntegrationUpdaterHashMismatchAbortsBeforeSwap(t *testing.T) {
	k := newUpdaterKit(t, "hash-mismatch", true)
	out, err := k.upd.Apply(context.Background())
	if out != app.OutcomeError {
		t.Errorf("out = %s, want error (aborted)", out)
	}
	// Aborted for the RIGHT reason: the downloaded bytes failed the dual-hash check.
	if !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("err = %v, want verify.ErrHashMismatch", err)
	}
	if k.liveContent(t) != "OLD-AGENT-BINARY" {
		t.Error("live binary changed despite a hash mismatch (AC-24)")
	}
	if !k.au.has("update.hash_mismatch") {
		t.Errorf("missing update.hash_mismatch (events=%v)", k.au.events)
	}
}

// ---- AC-25 + kill-switch + no-artifact: rejected, no swap -------------------

func TestIntegrationUpdaterRejections(t *testing.T) {
	for _, tc := range []struct{ fixture, event string }{
		{"downgrade-blocked", "update.downgrade_blocked"},
		{"update-not-allowed", "update.failed"},
		{"no-artifact", "update.failed"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			k := newUpdaterKit(t, tc.fixture, true)
			out, _ := k.upd.Apply(context.Background())
			if out != app.OutcomeRejected {
				t.Errorf("out = %s, want rejected", out)
			}
			if k.liveContent(t) != "OLD-AGENT-BINARY" {
				t.Error("live binary changed on a rejected update")
			}
			if !k.au.has(tc.event) {
				t.Errorf("missing %s (events=%v)", tc.event, k.au.events)
			}
		})
	}
}

// ---- AC-27: unhealthy update rolls back -------------------------------------

func TestIntegrationUpdaterUnhealthyRollsBack(t *testing.T) {
	k := newUpdaterKit(t, "valid", false) // agent never self-reports -> gate times out
	out, _ := k.upd.Apply(context.Background())
	if out != app.OutcomeRolledBack {
		t.Fatalf("out = %s, want rolled_back", out)
	}
	// rolled back to the prior binary (integrity-checked .bak restored)
	if k.liveContent(t) != "OLD-AGENT-BINARY" {
		t.Errorf("rollback did not restore the prior binary: %q", k.liveContent(t))
	}
	if !k.au.has("update.rolled_back") {
		t.Errorf("missing update.rolled_back (events=%v)", k.au.events)
	}
}

// ---- AC-28: crash recovery resumes from persisted updater_state.json --------

func TestIntegrationUpdaterCrashRecoveryResumes(t *testing.T) {
	k := newUpdaterKit(t, "valid", true)
	// Simulate a crash AT the swap boundary: drive the real primitives to a swapped
	// state (live = new, .bak recorded), persist HEALTH_CHECK, then a FRESH Apply
	// must resume forward and commit (no re-backup, no double swap).
	st := app.IdleState()
	st.FSMState = app.StateHealthCheck
	st.TargetVersion = "1.2.0"
	st.UpdateID = "upd_resume"
	st.HealthDeadline = k.clock.now().Add(90 * time.Second).Format(time.RFC3339)
	st.PendingReleasedAt = "2026-01-15T00:00:00Z"
	// reach the swapped on-disk state via the real swap primitives
	stageBackupSwap(t, k)
	st.BackupDigest = k.backupDigest
	if err := app.NewStateStore(k.stateDir).Save(st); err != nil {
		t.Fatal(err)
	}

	out, err := k.upd.Apply(context.Background()) // resumes from HEALTH_CHECK
	if out != app.OutcomeUpdated || err != nil {
		t.Fatalf("resume apply = %s, %v; want updated", out, err)
	}
	if k.liveContent(t) != string(mocksaas.ByName("valid").Artifact) {
		t.Error("resume did not keep the swapped binary / did not commit")
	}
}
