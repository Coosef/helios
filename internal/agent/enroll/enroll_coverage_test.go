package enroll_test

// Additional unit coverage for enroll branches not exercised by enroll_test.go:
// the constructor's per-dependency validation and default Idempotency-Key, the
// generic (non-401/409/426) transport-error mapping, persist failures on a
// non-secret field and on the license secret, and the advisory hardware-signal
// mapping. Reuses the fakes/builders from enroll_test.go (same package). Every
// case asserts a fail-closed behavior or a request-shape contract, not just lines.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// New rejects each individually-missing dependency.
func TestNewRejectsEachNilDependency(t *testing.T) {
	full := func() enroll.Deps {
		return enroll.Deps{
			Config: &config.Config{}, Identity: &fakeIdentity{}, Client: &fakeClient{},
			State: newFakeState(), Audit: newFakeAudit(),
		}
	}
	cases := []struct {
		name string
		mut  func(*enroll.Deps)
	}{
		{"nil config", func(d *enroll.Deps) { d.Config = nil }},
		{"nil identity", func(d *enroll.Deps) { d.Identity = nil }},
		{"nil client", func(d *enroll.Deps) { d.Client = nil }},
		{"nil state", func(d *enroll.Deps) { d.State = nil }},
		{"nil audit", func(d *enroll.Deps) { d.Audit = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := full()
			c.mut(&d)
			if _, err := enroll.New(d); err == nil {
				t.Errorf("New(%s) must error", c.name)
			}
		})
	}
	// With all deps present and no NewIdempotencyKey, New installs the default.
	if _, err := enroll.New(full()); err != nil {
		t.Errorf("New with default idempotency key must succeed: %v", err)
	}
}

// When NewIdempotencyKey is omitted, the default (uuid.NewRandom) generator is
// installed and used during a real enrollment.
func TestEnrollUsesDefaultIdempotencyKey(t *testing.T) {
	d := enroll.Deps{
		Config:   &config.Config{General: config.General{EnrollmentToken: config.Secret("bzt_default_key")}},
		Identity: &fakeIdentity{mat: testMaterial()},
		Client:   &fakeClient{resp: okResponse()},
		State:    newFakeState(),
		Audit:    newFakeAudit(),
		// NewIdempotencyKey intentionally omitted -> default uuid.NewRandom.
	}
	e, err := enroll.New(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Enroll(ctx()); err != nil {
		t.Fatalf("enroll with default idempotency key: %v", err)
	}
}

// A generic transport error (not 401/409/426) maps to ErrEnrollFailed, emits
// enroll.failed, persists nothing, and never leaks a secret to the log.
func TestGenericTransportErrorFailsClosed(t *testing.T) {
	var buf bytes.Buffer
	lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	k := newKit(t, func(d *enroll.Deps) { d.Log = lg })
	k.cl.resp, k.cl.err = nil, errors.New("connection refused")

	_, err = k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrEnrollFailed) {
		t.Errorf("err = %v, want ErrEnrollFailed", err)
	}
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, audit.EventEnrollFailed}) {
		t.Errorf("audit events = %v", got)
	}
	if len(k.st.puts) != 0 || len(k.st.secrets) != 0 {
		t.Error("nothing should be persisted on a transport failure")
	}
	if strings.Contains(buf.String(), "secrettoken") {
		t.Errorf("enrollment token leaked into logs on failure: %s", buf.String())
	}
}

// Persist failures fail closed with ErrPersist both on the first non-secret field
// and on the license secret, and the error path logs without leaking the secret.
func TestPersistFailureNonSecretAndLicense(t *testing.T) {
	t.Run("non-secret device_id put fails", func(t *testing.T) {
		k := newKit(t, nil)
		k.st.failPut[state.KeyDeviceID] = true
		res, err := k.enr.Enroll(ctx())
		if !errors.Is(err, enroll.ErrPersist) || res != nil {
			t.Errorf("err = %v, res = %v; want ErrPersist, nil", err, res)
		}
	})
	t.Run("license secret put fails", func(t *testing.T) {
		var buf bytes.Buffer
		lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
		if err != nil {
			t.Fatal(err)
		}
		k := newKit(t, func(d *enroll.Deps) { d.Log = lg })
		v := testLicenseV
		k.cl.resp = okResponse()
		k.cl.resp.LicenseBlob = &proto.LicenseBlob{Value: &v}
		k.st.failSecret[state.SecretLicenseBlob] = true

		res, err := k.enr.Enroll(ctx())
		if !errors.Is(err, enroll.ErrPersist) || res != nil {
			t.Errorf("err = %v, res = %v; want ErrPersist, nil", err, res)
		}
		if strings.Contains(buf.String(), testLicenseV) {
			t.Errorf("license secret leaked into logs on persist failure: %s", buf.String())
		}
	})
}

// hardwareSignals maps every advisory signal through to the request, and produces
// a nil block when none are present (AC-16: advisory only, never the primary key).
func TestHardwareSignalsFullAndEmptyMapping(t *testing.T) {
	t.Run("all signals mapped", func(t *testing.T) {
		k := newKit(t, nil)
		k.id.mat = testMaterial()
		k.id.mat.HardwareSignals = identity.HardwareSignals{
			MachineGUIDSHA256:       "sha256:m",
			PrimaryDiskSerialSHA256: "sha256:d",
			FirstNICMACSHA256:       "sha256:n",
			OS:                      "linux",
			OSVersion:               "9",
		}
		if _, err := k.enr.Enroll(ctx()); err != nil {
			t.Fatal(err)
		}
		fp := k.cl.gotBody.Fingerprint
		if fp == nil || fp.MachineGuidSha256 == nil || fp.PrimaryDiskSerialSha256 == nil ||
			fp.FirstNicMacSha256 == nil || fp.Os == nil || fp.OsVersion == nil {
			t.Errorf("not all hardware signals mapped through to the request: %+v", fp)
		}
	})
	t.Run("no signals -> nil fingerprint", func(t *testing.T) {
		k := newKit(t, nil)
		k.id.mat = testMaterial()
		k.id.mat.HardwareSignals = identity.HardwareSignals{}
		if _, err := k.enr.Enroll(ctx()); err != nil {
			t.Fatal(err)
		}
		if k.cl.gotBody.Fingerprint != nil {
			t.Errorf("fingerprint must be nil when no signals are present, got %+v", k.cl.gotBody.Fingerprint)
		}
	})
}
