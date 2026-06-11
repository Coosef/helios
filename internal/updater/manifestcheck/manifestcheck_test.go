package manifestcheck_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func ver(t *testing.T, s string) manifest.Version {
	t.Helper()
	v, err := manifest.ParseVersion(s)
	if err != nil {
		t.Fatalf("ParseVersion(%q): %v", s, err)
	}
	return v
}

// manifestMap builds a valid manifest (one linux/amd64 artifact) with overridable
// fields via the mutate callback.
func manifestMap(target, minSupported string, mutate func(map[string]any)) map[string]any {
	m := map[string]any{
		"schema_version":        1,
		"target_version":        target,
		"min_supported_version": minSupported,
		"artifacts": []any{map[string]any{
			"platform": "linux", "arch": "amd64", "url": "https://dl.example.com/agent",
			"size_bytes": 4096, "sha256": hex64, "blake3": hex64,
		}},
		"key_id":              "k1",
		"key_revocation_list": []any{},
		"signature":           "placeholder",
	}
	if mutate != nil {
		mutate(m)
	}
	return m
}

func signed(t *testing.T, m map[string]any, priv ed25519.PrivateKey) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	out, err := trusttest.SignManifest(raw, priv)
	if err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	return out
}

func keyPairAndSet(t *testing.T) (ed25519.PrivateKey, *trust.KeySet) {
	t.Helper()
	pub, priv := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(1))
	ks, err := trusttest.SingleKeySet("k1", pub)
	if err != nil {
		t.Fatal(err)
	}
	return priv, ks
}

// ---- Evaluate: the pure decision matrix ---------------------------------------

func TestEvaluateValidUpdateProceeds(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv)
	d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "linux", "amd64")
	if err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	if !d.Proceed || d.Reason != manifestcheck.ReasonOK {
		t.Errorf("Proceed=%v Reason=%q, want proceed/ok", d.Proceed, d.Reason)
	}
	if d.Artifact.URL == "" || d.TargetVersion.String() != "1.2.0" {
		t.Errorf("decision context missing: %+v", d)
	}
}

func TestEvaluateProceedIffNoError(t *testing.T) {
	// Invariant across a few outcomes: Proceed == (err == nil).
	priv, ks := keyPairAndSet(t)
	cases := [][]byte{
		signed(t, manifestMap("2.0.0", "1.0.0", nil), priv), // proceed
		signed(t, manifestMap("0.9.0", "0.5.0", nil), priv), // downgrade
	}
	for _, raw := range cases {
		d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.0.0"), "linux", "amd64")
		if d.Proceed != (err == nil) {
			t.Errorf("invariant broken: Proceed=%v err=%v", d.Proceed, err)
		}
	}
}

func TestEvaluateAntiRollback(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// target < current
	raw := signed(t, manifestMap("1.0.0", "0.9.0", nil), priv)
	if d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.5.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrDowngradeBlocked) || d.Proceed {
		t.Errorf("target<current: err=%v proceed=%v, want ErrDowngradeBlocked", err, d.Proceed)
	}
	// target == current (also blocked per AC-25 "<=")
	raw = signed(t, manifestMap("1.5.0", "1.0.0", nil), priv)
	if _, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.5.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrDowngradeBlocked) {
		t.Errorf("target==current: err=%v, want ErrDowngradeBlocked", err)
	}
}

func TestEvaluateEmergencyDowngradeHonored(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// signed allow_downgrade authorizes a strict downgrade
	raw := signed(t, manifestMap("1.0.0", "0.9.0", func(m map[string]any) { m["allow_downgrade"] = true }), priv)
	d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.5.0"), "linux", "amd64")
	if err != nil || !d.Proceed {
		t.Fatalf("signed allow_downgrade should permit downgrade: err=%v proceed=%v", err, d.Proceed)
	}
	if !d.EmergencyDowngrade {
		t.Error("EmergencyDowngrade flag not set")
	}
}

func TestEvaluateEmergencyDowngradeNotForEqualVersion(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// target == current with allow_downgrade is still blocked (not a downgrade)
	raw := signed(t, manifestMap("1.5.0", "1.0.0", func(m map[string]any) { m["allow_downgrade"] = true }), priv)
	if _, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.5.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrDowngradeBlocked) {
		t.Errorf("target==current with allow_downgrade: err=%v, want ErrDowngradeBlocked", err)
	}
}

func TestEvaluateBelowFloor(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// current (1.1.0) < min_supported (1.2.0), even though target (2.0.0) > current
	raw := signed(t, manifestMap("2.0.0", "1.2.0", nil), priv)
	if d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrBelowFloor) || d.Proceed {
		t.Errorf("below floor: err=%v proceed=%v, want ErrBelowFloor", err, d.Proceed)
	}
}

func TestEvaluateKillSwitch(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	raw := signed(t, manifestMap("1.2.0", "1.0.0", func(m map[string]any) { m["update_allowed"] = false }), priv)
	if d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrUpdateNotAllowed) || d.Proceed {
		t.Errorf("kill-switch: err=%v proceed=%v, want ErrUpdateNotAllowed", err, d.Proceed)
	}
	// update_allowed=true proceeds
	raw = signed(t, manifestMap("1.2.0", "1.0.0", func(m map[string]any) { m["update_allowed"] = true }), priv)
	if _, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "linux", "amd64"); err != nil {
		t.Errorf("update_allowed=true should proceed: %v", err)
	}
}

func TestEvaluateNoArtifactForPlatform(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv) // only linux/amd64
	if d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "windows", "arm64"); !errors.Is(err, manifestcheck.ErrNoArtifact) || d.Proceed {
		t.Errorf("no artifact: err=%v proceed=%v, want ErrNoArtifact", err, d.Proceed)
	}
}

func TestEvaluateCohortParsedNotEnforced(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// rollout_cohort_pct=0 would exclude everyone IF enforced — but Sprint 1 does
	// not enforce membership, so the update still proceeds and the value is exposed.
	raw := signed(t, manifestMap("1.2.0", "1.0.0", func(m map[string]any) { m["rollout_cohort_pct"] = 0 }), priv)
	d, err := manifestcheck.Evaluate(raw, ks, ver(t, "1.1.0"), "linux", "amd64")
	if err != nil || !d.Proceed {
		t.Fatalf("cohort must not be enforced in Sprint 1: err=%v proceed=%v", err, d.Proceed)
	}
	if d.RolloutCohortPct == nil || *d.RolloutCohortPct != 0 {
		t.Errorf("RolloutCohortPct not exposed: %v", d.RolloutCohortPct)
	}
}

func TestEvaluateVerificationFailureFailsClosed(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	// Tamper a field AFTER signing -> signature invalid (an unsigned source CANNOT
	// override the decision; the kill-switch/version come only from the signed form).
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv)
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	o["update_allowed"], _ = json.Marshal(true)    // inject a field post-sign
	o["target_version"], _ = json.Marshal("9.9.9") // and a higher version
	tampered, _ := json.Marshal(o)
	d, err := manifestcheck.Evaluate(tampered, ks, ver(t, "1.1.0"), "linux", "amd64")
	if !errors.Is(err, manifestcheck.ErrManifestRejected) || !errors.Is(err, verify.ErrSignatureInvalid) || d.Proceed {
		t.Errorf("tampered manifest: err=%v proceed=%v, want ErrManifestRejected+ErrSignatureInvalid", err, d.Proceed)
	}
}

func TestEvaluateUnknownKeyFailsClosed(t *testing.T) {
	priv, _ := keyPairAndSet(t)
	otherPub, _ := trusttest.DeterministicKeyPair(trusttest.SeedFromByte(9))
	wrongKS, _ := trusttest.SingleKeySet("k1", otherPub) // same key_id, different key
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv)
	if _, err := manifestcheck.Evaluate(raw, wrongKS, ver(t, "1.1.0"), "linux", "amd64"); !errors.Is(err, manifestcheck.ErrManifestRejected) {
		t.Errorf("wrong key: err=%v, want ErrManifestRejected", err)
	}
}

func TestEvaluateNilKeySetFailsClosed(t *testing.T) {
	priv, _ := keyPairAndSet(t)
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv)
	d, err := manifestcheck.Evaluate(raw, nil, ver(t, "1.1.0"), "linux", "amd64")
	if err == nil || d.Proceed {
		t.Errorf("nil key set must fail closed: err=%v proceed=%v", err, d.Proceed)
	}
}

// ---- Fetch: pinned T12 transport + T13-C3 cap ---------------------------------

func tlsServer(t *testing.T, h http.Handler) (*httptest.Server, *httpclient.Client) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	pin := httpclient.PinFromCertificate(srv.Certificate())
	c, err := httpclient.New(httpclient.Config{Pins: []string{pin}, ServerName: "127.0.0.1", MaxRetries: 0})
	if err != nil {
		t.Fatalf("httpclient.New: %v", err)
	}
	return srv, c
}

func TestFetchSuccess(t *testing.T) {
	body := []byte(`{"manifest":"bytes"}`)
	srv, c := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	got, err := manifestcheck.Fetch(context.Background(), c, srv.URL, 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestFetchNon200(t *testing.T) {
	srv, c := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	if _, err := manifestcheck.Fetch(context.Background(), c, srv.URL, 0); !errors.Is(err, manifestcheck.ErrFetch) {
		t.Errorf("404: err = %v, want ErrFetch", err)
	}
}

func TestFetchOversizedFailsClosed(t *testing.T) {
	srv, c := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	_, err := manifestcheck.Fetch(context.Background(), c, srv.URL, 16) // cap below body size
	if !errors.Is(err, manifestcheck.ErrFetch) || !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Errorf("oversized: err = %v, want ErrFetch+ErrResponseTooLarge", err)
	}
}

func TestFetchPinMismatchFailsClosed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	bogusPin := "sha256:" + "00000000000000000000000000000000000000000000000000000000000000ab"
	c, err := httpclient.New(httpclient.Config{Pins: []string{bogusPin}, ServerName: "127.0.0.1", MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestcheck.Fetch(context.Background(), c, srv.URL, 0); !errors.Is(err, manifestcheck.ErrFetch) || !errors.Is(err, httpclient.ErrPinMismatch) {
		t.Errorf("pin mismatch: err = %v, want ErrFetch+ErrPinMismatch", err)
	}
}

func TestFetchNilClient(t *testing.T) {
	if _, err := manifestcheck.Fetch(context.Background(), nil, "https://x", 0); !errors.Is(err, manifestcheck.ErrFetch) {
		t.Errorf("nil client: err = %v, want ErrFetch", err)
	}
}

// ---- Check: fetch + evaluate --------------------------------------------------

func TestCheckHappyPath(t *testing.T) {
	priv, ks := keyPairAndSet(t)
	raw := signed(t, manifestMap("1.2.0", "1.0.0", nil), priv)
	srv, c := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(raw)
	}))
	d, err := manifestcheck.Check(context.Background(), c, srv.URL, 0, ks, ver(t, "1.1.0"), "linux", "amd64")
	if err != nil || !d.Proceed {
		t.Fatalf("Check happy path: err=%v proceed=%v", err, d.Proceed)
	}
}

func TestCheckFetchFailureFailsClosed(t *testing.T) {
	_, ks := keyPairAndSet(t)
	srv, c := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	d, err := manifestcheck.Check(context.Background(), c, srv.URL, 0, ks, ver(t, "1.1.0"), "linux", "amd64")
	if !errors.Is(err, manifestcheck.ErrFetch) || d.Proceed || d.Reason != manifestcheck.ReasonFetchFailed {
		t.Errorf("fetch failure: err=%v proceed=%v reason=%q, want ErrFetch/fetch_failed", err, d.Proceed, d.Reason)
	}
}
