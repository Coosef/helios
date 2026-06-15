package security

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	updapp "github.com/beyzbackup/beyz-backup/internal/updater/app"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// AC-12: no secret value ever reaches agent.log or security.log after a full
// enroll + heartbeat. (Re-exercises the redaction sink end-to-end, not just the
// Secret wrapper unit test.)
func TestNeg_SecretsNeverLogged(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, srv, pin)

	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	k.seedSessionToken(t) // so the heartbeat actually sends the bearer
	if _, err := k.beater(t).Beat(context.Background()); err != nil {
		t.Fatalf("beat: %v", err)
	}

	// Sanity: the secrets were genuinely SENT, or the grep below would be vacuous.
	if req, ok := f.lastTo("/v1/enroll"); !ok || !strings.Contains(req.Body, enrollToken) {
		t.Fatal("enroll token was not sent in the request body (test would be vacuous)")
	}
	if req, ok := f.lastTo("/heartbeat"); !ok || !strings.Contains(strings.Join(req.Header["Authorization"], ""), sessionToken) {
		t.Fatal("heartbeat did not send the bearer session token (test would be vacuous)")
	}

	k.flushLogs(t) // flush/close before grepping (freeze #7)
	for _, lf := range []struct{ name, path string }{
		{"agent.log", k.agentLog}, {"security.log", k.securityLog},
	} {
		content := readFileOrEmpty(t, lf.path)
		if content == "" {
			t.Errorf("%s is empty (expected events to be written)", lf.name)
			continue
		}
		for _, secret := range []string{enrollToken, sessionToken} {
			if strings.Contains(content, secret) {
				t.Errorf("SECRET leaked into %s: %q", lf.name, secret)
			}
		}
	}
}

// AC-34: a control-channel connection to a server presenting a non-pinned cert is
// refused at the TLS layer (the request never reaches the server).
func TestNeg_SPKIPinMismatchRefused(t *testing.T) {
	f, srv, _ := newFakeSaaS(t)
	wrongPin := "sha256:" + strings.Repeat("ab", 32) // not the server's SPKI
	k := newAgentKitPinned(t, srv, wrongPin)

	_, err := k.enroller(t).Enroll(context.Background())
	if err == nil {
		t.Fatal("enroll against a non-pinned cert must be refused")
	}
	// The pin check fails during the TLS handshake, so the request never reaches the
	// app handler (proves the pin is enforced at the transport, not after).
	if _, ok := f.lastTo("/v1/enroll"); ok {
		t.Error("a non-pinned connection reached the server handler (SPKI pin not enforced at handshake)")
	}
}

// AC-15: re-running enrollment with an already-consumed token returns 409, fails
// closed (terminal ErrTokenRejected), and does NOT corrupt existing enrolled state.
func TestNeg_TokenReplay409FailsClosed(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	k := newAgentKit(t, srv, pin)

	if _, err := k.enroller(t).Enroll(context.Background()); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	certBefore, _ := k.store.Get(state.KeyCertificate)
	devBefore, _ := k.store.Get(state.KeyDeviceID)
	if len(certBefore) == 0 || len(devBefore) == 0 {
		t.Fatal("first enroll did not persist credential")
	}

	f.set(func(s *fakeSaaS) { s.enrollStatus = 409 }) // token now consumed

	_, err := k.enroller(t).Enroll(context.Background())
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Fatalf("replay enroll: err = %v, want ErrTokenRejected", err)
	}
	if certAfter, _ := k.store.Get(state.KeyCertificate); string(certAfter) != string(certBefore) {
		t.Error("existing certificate was overwritten on a 409 replay")
	}
	if devAfter, _ := k.store.Get(state.KeyDeviceID); string(devAfter) != string(devBefore) {
		t.Error("existing device_id was overwritten on a 409 replay")
	}
}

// AC clone-detection: the server (here the fakeSaaS) flags a cloned device with a
// 409; the agent surfaces it terminally and does NOT persist credential.
func TestNeg_ClonedStateDetectable(t *testing.T) {
	f, srv, pin := newFakeSaaS(t)
	f.set(func(s *fakeSaaS) { s.enrollStatus = 409; s.enrollProblem = "device clone detected" })
	k := newAgentKit(t, srv, pin)

	_, err := k.enroller(t).Enroll(context.Background())
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Fatalf("clone-flagged enroll: err = %v, want a terminal rejection", err)
	}
	if got, _ := k.store.Get(state.KeyDeviceID); len(got) != 0 {
		t.Errorf("device_id persisted despite the clone rejection: %q", got)
	}
	// The agent ships the advisory, privacy-preserving hardware fingerprint that
	// lets the server detect a clone (collected only on linux/windows).
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if req, ok := f.lastTo("/v1/enroll"); ok && !strings.Contains(req.Body, "fingerprint") {
			t.Error("enroll request carries no advisory hardware fingerprint for clone detection")
		}
	}
}

// AC-23/24/25/29: a forged-signature / revoked-key / hash-mismatch / downgrade
// manifest is REJECTED by the real updater FSM and NOTHING is staged or swapped
// (the live binary is byte-unchanged and the service is never (re)started). The
// unknown-key rejection has its own end-to-end test below
// (TestNeg_UnknownSigningKeyRejectedNothingSwapped), since it is exercised by
// driving an otherwise-valid manifest under a foreign trust anchor.
func TestNeg_ManifestRejectionsNothingSwapped(t *testing.T) {
	for _, fixture := range []string{
		"invalid-signature", // forged Ed25519 signature (AC-29)
		"revoked-key",       // key_id in the manifest revocation list (AC-23)
		"hash-mismatch",     // artifact hash != signed manifest hash (AC-24)
		"downgrade-blocked", // target_version <= current (AC-25)
	} {
		t.Run(fixture, func(t *testing.T) {
			k := newUpdaterKit(t, fixture)
			out, _ := k.upd.Apply(context.Background())

			// Never applied: a rejected/forged/hash-mismatched manifest must not update.
			if out == updapp.OutcomeUpdated {
				t.Fatalf("%s was APPLIED; a rejected manifest must never update", fixture)
			}
			// THE security invariant: the untrusted artifact never becomes the live
			// binary (manifest-level rejections never download; the hash-mismatch
			// aborts pre-swap, after which the FSM safely restarts the UNCHANGED agent).
			if got := k.liveContent(t); got != liveBinaryContent {
				t.Errorf("%s: live binary was swapped to the untrusted artifact despite rejection", fixture)
			}
			// The FSM never swapped (the swap.swapped audit event is absent).
			if k.au.has("update.swapped") {
				t.Errorf("%s: the FSM emitted update.swapped despite rejection", fixture)
			}
		})
	}
}

// AC-23: a manifest signed by an UNKNOWN key_id (one not in the trust anchor —
// distinct from an embedded/manifest-revoked key) is rejected by the REAL FSM and
// nothing is staged or swapped. It is exercised by serving the otherwise-VALID
// fixture (which would proceed under the real anchor) under a FOREIGN trust anchor,
// so the only thing that turns proceed->reject is the unrecognized key_id — proven
// by asserting the decision layer rejects specifically with trust.ErrUnknownKey.
func TestNeg_UnknownSigningKeyRejectedNothingSwapped(t *testing.T) {
	foreign := foreignKeySet(t)
	k := newUpdaterKitWithKeys(t, "valid", foreign)

	// Genuineness: the served (otherwise-valid) manifest is rejected SPECIFICALLY
	// because its key_id is unknown to this anchor — not for downgrade/hash/etc.
	raw := fetchManifest(t, k)
	baseline, err := manifest.ParseVersion(fxBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if _, derr := manifestcheck.Evaluate(raw, foreign, baseline, fxPlatform, fxArch); !errors.Is(derr, trust.ErrUnknownKey) {
		t.Fatalf("decision error = %v, want trust.ErrUnknownKey (test would not be exercising unknown-key)", derr)
	}

	out, _ := k.upd.Apply(context.Background())
	if out == updapp.OutcomeUpdated {
		t.Fatal("an unknown-key manifest was APPLIED; an untrusted key_id must never update")
	}
	if got := k.liveContent(t); got != liveBinaryContent {
		t.Error("live binary was swapped to the untrusted artifact despite an unknown signing key")
	}
	if k.au.has("update.swapped") {
		t.Error("the FSM emitted update.swapped despite an unknown signing key")
	}
}

// foreignKeySet builds a valid (non-empty, one active key) trust anchor whose
// key_id matches no fixture manifest, so resolving any fixture's key_id against it
// fails closed with trust.ErrUnknownKey. The key is derived from a fixed seed for
// determinism; it is never used to sign (the key_id never matches), so its curve
// value is immaterial — only its size and unrecognized id matter.
func foreignKeySet(t *testing.T) *trust.KeySet {
	t.Helper()
	pub := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	ks, err := trust.NewKeySet([]trust.KeyEntry{{KeyID: "foreign-anchor-not-in-any-manifest", PublicKey: pub}})
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

// fetchManifest GETs the fixture manifest bytes from the in-memory mock server.
func fetchManifest(t *testing.T, k *updaterKit) []byte {
	t.Helper()
	resp, err := k.server.Client().Get(k.server.ManifestURL())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// AC-17: a tampered protected blob fails CLOSED (auth-tag check) rather than
// returning garbage plaintext. (The InsecureTestProtector mirrors the production
// DPAPI authenticated-encryption contract.)
func TestNeg_StateProtectorTamperFailsClosed(t *testing.T) {
	prot, err := state.NewInsecureTestProtector()
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("agent-private-key-bytes")
	blob, err := prot.Protect(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) == string(secret) {
		t.Fatal("protector returned plaintext (not encrypted)")
	}
	// flip a byte in the ciphertext region.
	tampered := append([]byte(nil), blob...)
	tampered[len(tampered)-1] ^= 0xFF
	out, err := prot.Unprotect(tampered)
	if err == nil {
		t.Fatalf("tampered blob unwrapped without error -> got %q (must fail closed)", out)
	}
	// the genuine blob still round-trips.
	if got, err := prot.Unprotect(blob); err != nil || string(got) != string(secret) {
		t.Fatalf("genuine blob failed to round-trip: got=%q err=%v", got, err)
	}
}
