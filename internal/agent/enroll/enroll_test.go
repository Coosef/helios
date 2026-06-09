package enroll_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

const (
	testToken    = "bzt_secrettoken0001"
	testSession  = "ast_secretsession0001"
	testGUID     = "11111111-2222-3333-4444-555555555555"
	testLicenseV = "licenseblobsecret0001"
)

func ctx() context.Context { return context.Background() }

// ---- fakes ------------------------------------------------------------------

type fakeIdentity struct {
	mat *identity.Material
	err error
}

func (f *fakeIdentity) Ensure() (*identity.Material, error) { return f.mat, f.err }

type fakeClient struct {
	resp    *proto.EnrollResponse
	err     error
	gotBody proto.EnrollRequest
	gotOpts int
	calls   int
}

func (f *fakeClient) Enroll(_ context.Context, body proto.EnrollRequest, opts ...saasclient.RequestOption) (*proto.EnrollResponse, error) {
	f.calls++
	f.gotBody = body
	f.gotOpts = len(opts)
	return f.resp, f.err
}

type fakeState struct {
	puts       map[string][]byte
	secrets    map[string][]byte
	failPut    map[string]bool
	failSecret map[string]bool
}

func newFakeState() *fakeState {
	return &fakeState{
		puts: map[string][]byte{}, secrets: map[string][]byte{},
		failPut: map[string]bool{}, failSecret: map[string]bool{},
	}
}

func (s *fakeState) Put(k string, v []byte) error {
	if s.failPut[k] {
		return fmt.Errorf("put %s failed", k)
	}
	s.puts[k] = append([]byte(nil), v...)
	return nil
}

func (s *fakeState) PutSecret(k string, v []byte) error {
	if s.failSecret[k] {
		return fmt.Errorf("putsecret %s failed", k)
	}
	s.secrets[k] = append([]byte(nil), v...)
	return nil
}

type fakeAudit struct {
	events []audit.Event
	failOn map[string]bool
}

func newFakeAudit() *fakeAudit { return &fakeAudit{failOn: map[string]bool{}} }

func (a *fakeAudit) Emit(ev audit.Event) (audit.Record, error) {
	a.events = append(a.events, ev)
	if a.failOn[ev.EventType] {
		return audit.Record{}, fmt.Errorf("audit %s failed", ev.EventType)
	}
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

func testMaterial() *identity.Material {
	return &identity.Material{
		DeviceGUID:      testGUID,
		SPKIFingerprint: "sha256:" + strings.Repeat("ab", 32),
		CSRPEM:          []byte("-----BEGIN CERTIFICATE REQUEST-----\nMIIB\n-----END CERTIFICATE REQUEST-----\n"),
		HardwareSignals: identity.HardwareSignals{
			MachineGUIDSHA256: "sha256:deadbeef",
			OS:                "linux",
			OSVersion:         "test-1.0",
		},
	}
}

func okResponse() *proto.EnrollResponse {
	pid := proto.ParentOrgId("msp_parent01")
	lid := proto.LocationId("loc_site_a")
	return &proto.EnrollResponse{
		DeviceId:            "dev_abc123",
		TenantId:            "tnt_xyz",
		ParentOrgId:         &pid,
		Region:              "eu-central-1",
		LocationId:          &lid,
		AgentCertificatePem: "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n",
		AgentSessionToken:   testSession,
	}
}

type kit struct {
	id    *fakeIdentity
	cl    *fakeClient
	st    *fakeState
	au    *fakeAudit
	keyN  int
	enr   *enroll.Enroller
	token string
}

// newKit wires fakes into an Enroller. mutate may adjust Deps before construction.
func newKit(t *testing.T, mutate func(*enroll.Deps)) *kit {
	t.Helper()
	k := &kit{
		id:    &fakeIdentity{mat: testMaterial()},
		cl:    &fakeClient{resp: okResponse()},
		st:    newFakeState(),
		au:    newFakeAudit(),
		token: testToken,
	}
	d := enroll.Deps{
		Config: &config.Config{General: config.General{
			EnrollmentToken: config.Secret(k.token),
			TenantID:        "tnt_hint",
			Region:          "eu-hint",
		}},
		Identity: k.id, Client: k.cl, State: k.st, Audit: k.au,
		NewIdempotencyKey: func() (uuid.UUID, error) {
			k.keyN++
			return uuid.MustParse("99999999-8888-7777-6666-555555555555"), nil
		},
	}
	if mutate != nil {
		mutate(&d)
	}
	e, err := enroll.New(d)
	if err != nil {
		t.Fatalf("enroll.New: %v", err)
	}
	k.enr = e
	return k
}

// ---- tests ------------------------------------------------------------------

func TestNewValidation(t *testing.T) {
	if _, err := enroll.New(enroll.Deps{}); err == nil {
		t.Error("New(empty) should error (nil config)")
	}
	cfg := &config.Config{}
	if _, err := enroll.New(enroll.Deps{Config: cfg, Identity: &fakeIdentity{}, Client: &fakeClient{}, State: newFakeState()}); err == nil {
		t.Error("New(missing audit) should error")
	}
}

func TestEnrollNoToken(t *testing.T) {
	k := newKit(t, func(d *enroll.Deps) {
		d.Config = &config.Config{} // empty EnrollmentToken
	})
	if _, err := k.enr.Enroll(ctx()); !errors.Is(err, enroll.ErrNoEnrollmentToken) {
		t.Errorf("err = %v, want ErrNoEnrollmentToken", err)
	}
	if k.cl.calls != 0 {
		t.Error("must not contact the server without a token")
	}
	if len(k.au.events) != 0 {
		t.Error("must not emit audit without a token")
	}
}

func TestEnrollSuccessPersistsState(t *testing.T) {
	k := newKit(t, nil)
	res, err := k.enr.Enroll(ctx())
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	// Result.
	want := enroll.Result{DeviceID: "dev_abc123", TenantID: "tnt_xyz", ParentOrgID: "msp_parent01", Region: "eu-central-1", LocationID: "loc_site_a"}
	if *res != want {
		t.Errorf("result = %+v, want %+v", *res, want)
	}
	// Non-secret state.
	assertPut(t, k.st, state.KeyDeviceID, "dev_abc123")
	assertPut(t, k.st, state.KeyTenantID, "tnt_xyz")
	assertPut(t, k.st, state.KeyParentOrgID, "msp_parent01")
	assertPut(t, k.st, state.KeyRegion, "eu-central-1")
	assertPut(t, k.st, state.KeyLocationID, "loc_site_a")
	assertPut(t, k.st, state.KeyCertificate, "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n")
	// Secret state.
	assertSecret(t, k.st, state.SecretSessionToken, testSession)
	// Audit.
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, audit.EventEnrollSucceeded}) {
		t.Errorf("audit events = %v", got)
	}
	// Request construction: advisory hints + identity + token in body.
	if k.cl.gotBody.EnrollmentToken == nil || *k.cl.gotBody.EnrollmentToken != testToken {
		t.Error("enrollment token not placed in request body")
	}
	if k.cl.gotBody.DeviceGuid.String() != testGUID {
		t.Errorf("device_guid = %s", k.cl.gotBody.DeviceGuid)
	}
	if k.cl.gotBody.SpkiSha256 == nil || k.cl.gotBody.TenantId == nil || k.cl.gotBody.Region == nil {
		t.Error("expected spki_sha256 + advisory tenant_id/region in request")
	}
	if k.cl.gotBody.RecoveryPolicy != proto.Escrowed {
		t.Errorf("recovery_policy = %q, want escrowed", k.cl.gotBody.RecoveryPolicy)
	}
}

func TestLocationOptional(t *testing.T) {
	t.Run("advisory hint sent + authoritative persisted", func(t *testing.T) {
		k := newKit(t, func(d *enroll.Deps) { d.AdvisoryLocationID = "loc_operator_hint" })
		if _, err := k.enr.Enroll(ctx()); err != nil {
			t.Fatal(err)
		}
		if k.cl.gotBody.LocationId == nil || *k.cl.gotBody.LocationId != "loc_operator_hint" {
			t.Errorf("advisory location_id not sent: %v", k.cl.gotBody.LocationId)
		}
		assertPut(t, k.st, state.KeyLocationID, "loc_site_a") // server-authoritative wins
	})
	t.Run("absent when neither advised nor returned", func(t *testing.T) {
		k := newKit(t, nil)
		k.cl.resp = okResponse()
		k.cl.resp.LocationId = nil // server returns none
		res, err := k.enr.Enroll(ctx())
		if err != nil {
			t.Fatal(err)
		}
		if k.cl.gotBody.LocationId != nil {
			t.Error("location_id must be omitted from request when not advised")
		}
		if _, ok := k.st.puts[state.KeyLocationID]; ok {
			t.Error("location_id must not be persisted when the server returns none")
		}
		if res.LocationID != "" {
			t.Errorf("result.LocationID = %q, want empty", res.LocationID)
		}
	})
}

func TestParentOrgPersistence(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		k := newKit(t, nil)
		res, err := k.enr.Enroll(ctx())
		if err != nil {
			t.Fatal(err)
		}
		assertPut(t, k.st, state.KeyParentOrgID, "msp_parent01")
		if res.ParentOrgID != "msp_parent01" {
			t.Errorf("ParentOrgID = %q", res.ParentOrgID)
		}
	})
	t.Run("absent (direct tenant)", func(t *testing.T) {
		k := newKit(t, nil)
		k.cl.resp = okResponse()
		k.cl.resp.ParentOrgId = nil
		res, err := k.enr.Enroll(ctx())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := k.st.puts[state.KeyParentOrgID]; ok {
			t.Error("parent_org_id must not be persisted for a direct tenant")
		}
		if res.ParentOrgID != "" {
			t.Errorf("ParentOrgID = %q, want empty", res.ParentOrgID)
		}
	})
}

func TestTokenStoredAsSecretNotPlaintext(t *testing.T) {
	k := newKit(t, nil)
	if _, err := k.enr.Enroll(ctx()); err != nil {
		t.Fatal(err)
	}
	// Session token is a secret.
	assertSecret(t, k.st, state.SecretSessionToken, testSession)
	// It must never appear in the plaintext (Put) bucket, under any key.
	for key, v := range k.st.puts {
		if strings.Contains(string(v), testSession) {
			t.Errorf("session token leaked into plaintext key %q", key)
		}
		if strings.Contains(string(v), testToken) {
			t.Errorf("enrollment token leaked into plaintext key %q", key)
		}
	}
	// The single-use enrollment token is consumed, never persisted at all.
	if _, ok := k.st.secrets[state.SecretSessionToken]; !ok {
		t.Error("session token not stored as secret")
	}
	for key := range k.st.puts {
		if key == state.SecretSessionToken {
			t.Error("session token stored under the non-secret API")
		}
	}
}

func TestLicenseBlobStoredAsSecret(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		k := newKit(t, nil)
		v := testLicenseV
		k.cl.resp = okResponse()
		k.cl.resp.LicenseBlob = &proto.LicenseBlob{Value: &v}
		if _, err := k.enr.Enroll(ctx()); err != nil {
			t.Fatal(err)
		}
		blob, ok := k.st.secrets[state.SecretLicenseBlob]
		if !ok {
			t.Fatal("license_blob not stored as secret")
		}
		if !strings.Contains(string(blob), testLicenseV) {
			t.Errorf("license_blob secret missing value: %s", blob)
		}
		if _, ok := k.st.puts[state.SecretLicenseBlob]; ok {
			t.Error("license_blob must not be in the plaintext bucket")
		}
	})
	t.Run("absent", func(t *testing.T) {
		k := newKit(t, nil) // okResponse has no license blob
		if _, err := k.enr.Enroll(ctx()); err != nil {
			t.Fatal(err)
		}
		if _, ok := k.st.secrets[state.SecretLicenseBlob]; ok {
			t.Error("license_blob secret written when none returned")
		}
	})
}

func TestTokenRejected401(t *testing.T) {
	k := newKit(t, nil)
	k.cl.resp, k.cl.err = nil, saasclient.ErrUnauthorized
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Errorf("err = %v, want ErrTokenRejected", err)
	}
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, "enroll.token_rejected"}) {
		t.Errorf("audit events = %v", got)
	}
	if len(k.st.puts) != 0 || len(k.st.secrets) != 0 {
		t.Error("no state should be persisted on rejection")
	}
}

func TestConflict409(t *testing.T) {
	k := newKit(t, nil)
	k.cl.resp, k.cl.err = nil, saasclient.ErrConflict
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Errorf("err = %v, want ErrTokenRejected (409)", err)
	}
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, "enroll.token_rejected"}) {
		t.Errorf("audit events = %v", got)
	}
}

func TestUpgradeRequired426(t *testing.T) {
	k := newKit(t, nil)
	k.cl.resp, k.cl.err = nil, saasclient.ErrUpgradeRequired
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrUpgradeRequired) {
		t.Errorf("err = %v, want ErrUpgradeRequired", err)
	}
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, audit.EventEnrollFailed}) {
		t.Errorf("audit events = %v", got)
	}
}

func TestPersistFailure(t *testing.T) {
	k := newKit(t, nil)
	k.st.failSecret[state.SecretSessionToken] = true // server succeeded, local persist fails
	res, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrPersist) {
		t.Errorf("err = %v, want ErrPersist", err)
	}
	if res != nil {
		t.Error("result must be nil on persist failure")
	}
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, audit.EventEnrollFailed}) {
		t.Errorf("audit events = %v", got)
	}
}

func TestAttemptAuditFailsClosed(t *testing.T) {
	k := newKit(t, nil)
	k.au.failOn[audit.EventEnrollAttempt] = true
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrAudit) {
		t.Errorf("err = %v, want ErrAudit", err)
	}
	if k.cl.calls != 0 {
		t.Error("must not contact the server if the attempt cannot be audited")
	}
}

func TestAuditFailureDoesNotHideEnrollFailure(t *testing.T) {
	k := newKit(t, nil)
	k.cl.resp, k.cl.err = nil, saasclient.ErrUnauthorized // enrollment fails
	k.au.failOn["enroll.token_rejected"] = true           // AND the audit emit fails
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrTokenRejected) {
		t.Errorf("enrollment failure hidden: %v", err)
	}
	if !errors.Is(err, enroll.ErrAudit) {
		t.Errorf("audit failure silently dropped: %v", err)
	}
}

func TestIdentityFailure(t *testing.T) {
	k := newKit(t, nil)
	k.id.mat, k.id.err = nil, errors.New("keygen boom")
	_, err := k.enr.Enroll(ctx())
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Errorf("err = %v, want identity error", err)
	}
	if k.cl.calls != 0 || len(k.au.events) != 0 {
		t.Error("identity failure must precede any server call or audit")
	}
}

func TestInvalidDeviceGUID(t *testing.T) {
	k := newKit(t, nil)
	k.id.mat = testMaterial()
	k.id.mat.DeviceGUID = "not-a-uuid" // breaks request construction
	_, err := k.enr.Enroll(ctx())
	if !errors.Is(err, enroll.ErrEnrollFailed) {
		t.Errorf("err = %v, want ErrEnrollFailed", err)
	}
	if k.cl.calls != 0 {
		t.Error("must not contact the server when the request cannot be built")
	}
	// The attempt was audited before the build failed.
	if got := k.au.types(); !equalSeq(got, []string{audit.EventEnrollAttempt, audit.EventEnrollFailed}) {
		t.Errorf("audit events = %v", got)
	}
}

// The success path durably persists BEFORE emitting enroll.succeeded; if that
// emit fails, the enrollment is still durable so a populated Result is returned,
// and the audit failure is surfaced (never hidden) without masquerading as an
// enrollment failure.
func TestSucceededAuditFailureSurfacedButDurable(t *testing.T) {
	k := newKit(t, nil)
	k.au.failOn[audit.EventEnrollSucceeded] = true
	res, err := k.enr.Enroll(ctx())
	if res == nil {
		t.Fatal("durable enrollment must still return a Result")
	}
	want := enroll.Result{DeviceID: "dev_abc123", TenantID: "tnt_xyz", ParentOrgID: "msp_parent01", Region: "eu-central-1", LocationID: "loc_site_a"}
	if *res != want {
		t.Errorf("result = %+v, want %+v", *res, want)
	}
	if !errors.Is(err, enroll.ErrAudit) {
		t.Errorf("audit failure not surfaced: %v", err)
	}
	if errors.Is(err, enroll.ErrEnrollFailed) || errors.Is(err, enroll.ErrPersist) || errors.Is(err, enroll.ErrTokenRejected) {
		t.Errorf("audit failure must not masquerade as an enrollment failure: %v", err)
	}
	// The credential was persisted before the failing audit emit.
	assertSecret(t, k.st, state.SecretSessionToken, testSession)
}

func TestIdempotencyKeySupplied(t *testing.T) {
	k := newKit(t, nil)
	if _, err := k.enr.Enroll(ctx()); err != nil {
		t.Fatal(err)
	}
	if k.keyN != 1 {
		t.Errorf("idempotency key generated %d times, want 1 (stable per attempt)", k.keyN)
	}
	if k.cl.gotOpts != 1 {
		t.Errorf("Enroll called with %d options, want 1 (WithIdempotencyKey)", k.cl.gotOpts)
	}
}

func TestNoSecretLeakInLogsAndAudit(t *testing.T) {
	var buf bytes.Buffer
	lg, err := logging.New(logging.Options{Writer: &buf, Format: "json", Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	k := newKit(t, func(d *enroll.Deps) { d.Log = lg })
	v := testLicenseV
	k.cl.resp = okResponse()
	k.cl.resp.LicenseBlob = &proto.LicenseBlob{Value: &v}
	if _, err := k.enr.Enroll(ctx()); err != nil {
		t.Fatal(err)
	}

	logs := buf.String()
	for _, secret := range []string{"secrettoken", "secretsession", testLicenseV} {
		if strings.Contains(logs, secret) {
			t.Errorf("secret %q leaked into logs: %s", secret, logs)
		}
	}
	// Audit detail (pre-redaction, at the source) must carry no secrets.
	for _, ev := range k.au.events {
		for key, val := range ev.Detail {
			s := fmt.Sprintf("%v", val)
			for _, secret := range []string{"secrettoken", "secretsession", testLicenseV} {
				if strings.Contains(s, secret) {
					t.Errorf("secret %q leaked into audit detail %q=%v", secret, key, val)
				}
			}
		}
	}
}

// ---- helpers ----------------------------------------------------------------

func assertPut(t *testing.T, s *fakeState, key, want string) {
	t.Helper()
	got, ok := s.puts[key]
	if !ok {
		t.Errorf("non-secret key %q not persisted", key)
		return
	}
	if string(got) != want {
		t.Errorf("%q = %q, want %q", key, got, want)
	}
}

func assertSecret(t *testing.T, s *fakeState, key, want string) {
	t.Helper()
	got, ok := s.secrets[key]
	if !ok {
		t.Errorf("secret key %q not persisted", key)
		return
	}
	if string(got) != want {
		t.Errorf("secret %q = %q, want %q", key, got, want)
	}
}

func equalSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
