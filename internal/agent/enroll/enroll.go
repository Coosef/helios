// Package enroll implements the agent enrollment use-case (S1-T14): it composes
// identity, the SaaS client, the protected state store, and the audit emitter to
// turn a single-use enrollment token into a persisted device credential.
//
// This is a USE-CASE ORCHESTRATION layer only. It contains no transport logic
// (that is S1-T12/T13), no certificate renewal, heartbeat, task polling,
// scheduler, backup/restore, or RBAC, and it adds no business logic to the SaaS
// client. Per the ADRs:
//   - Identity is local-first (ADR-003): device_guid, an ECDSA P-256 key, a CSR,
//     and the SPKI fingerprint are produced by internal/agent/identity; the
//     private key never leaves the state store and is never logged.
//   - tenant_id / parent_org_id / region are server-authoritative and bound into
//     the agent certificate (ADR-003); the request carries them only as advisory
//     hints.
//   - location_id is advisory IN (an operator hint) and server-authoritative OUT,
//     never certificate-bound (ADR-006).
//   - Secrets (agent_session_token, license_blob) are written through the state
//     store's secret API (DPAPI-wrapped at rest) and NEVER logged; cert and IDs
//     are written plaintext through the non-secret API. The store's key
//     classification is honored, never bypassed.
package enroll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/identity"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/state"
	"github.com/beyzbackup/beyz-backup/internal/transport/saasclient"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
	"github.com/beyzbackup/beyz-backup/pkg/wireversion"
)

// Typed errors. Match with errors.Is. A returned error may aggregate more than
// one cause (e.g. an enrollment failure AND an audit-emit failure) via
// errors.Join so that an audit problem never silently hides the real outcome.
var (
	// ErrNoEnrollmentToken means no enrollment token was configured (it is sourced
	// only from BEYZ_ENROLLMENT_TOKEN; SEC-2). Nothing was attempted.
	ErrNoEnrollmentToken = errors.New("enroll: no enrollment token configured")
	// ErrTokenRejected means the server refused the enrollment token (HTTP 401
	// unauthorized or 409 already-consumed). The caller obtains a fresh token.
	ErrTokenRejected = errors.New("enroll: enrollment token rejected")
	// ErrUpgradeRequired propagates HTTP 426: the agent/protocol is too old; the
	// caller must stop and route to the updater (ADR-004).
	ErrUpgradeRequired = errors.New("enroll: protocol upgrade required")
	// ErrEnrollFailed is any other enrollment failure (transport/build/unexpected).
	ErrEnrollFailed = errors.New("enroll: enrollment failed")
	// ErrPersist means the server enrolled the device but persisting the returned
	// credential to the state store failed — a partial failure the caller must
	// surface (a retry is safe; the same idempotency semantics apply server-side).
	ErrPersist = errors.New("enroll: persisting enrollment state failed")
	// ErrAudit means an audit record could not be emitted. It never replaces the
	// primary enrollment outcome; it is joined alongside it.
	ErrAudit = errors.New("enroll: audit emit failed")
)

// eventEnrollTokenRejected is part of the frozen audit vocabulary (T09) but has
// no exported constant in the audit package.
const eventEnrollTokenRejected = "enroll.token_rejected"

// actorInstaller marks enrollment as the installer/operator-initiated bootstrap.
const actorInstaller = "installer"

// Identity produces the local-first enrollment material (ADR-003).
// Satisfied by *identity.Manager.
type Identity interface {
	Ensure() (*identity.Material, error)
}

// EnrollClient performs the typed enrollment request/response (S1-T13).
// Satisfied by *saasclient.Client.
type EnrollClient interface {
	Enroll(ctx context.Context, body proto.EnrollRequest, opts ...saasclient.RequestOption) (*proto.EnrollResponse, error)
}

// StateWriter persists enrollment results. Non-secret values go through Put;
// secrets go through PutSecret (wrapped at rest). The store enforces the
// secret/non-secret key classification — this layer never bypasses it.
// Satisfied by *state.Store.
type StateWriter interface {
	Put(key string, value []byte) error
	PutSecret(key string, value []byte) error
}

// AuditEmitter records hash-chained audit events (S1-T09).
// Satisfied by *audit.Emitter.
type AuditEmitter interface {
	Emit(ev audit.Event) (audit.Record, error)
}

// Deps bundles the enrollment collaborators and inputs.
type Deps struct {
	// Config supplies the enrollment token (secret) and the advisory tenant/region
	// hints. Required.
	Config *config.Config
	// Identity, Client, State, Audit are the composed collaborators. Required.
	Identity Identity
	Client   EnrollClient
	State    StateWriter
	Audit    AuditEmitter
	// Log is optional (nil disables operational logging). It never receives
	// secrets; the logging package additionally redacts tokens at the sink.
	Log *logging.Logger
	// AdvisoryLocationID is an optional operator-chosen site hint (ADR-006); it is
	// sent only as an advisory request value and omitted when empty.
	AdvisoryLocationID string
	// NewIdempotencyKey generates the per-attempt Idempotency-Key. Defaults to a
	// random UUIDv4. Overridable for tests.
	NewIdempotencyKey func() (uuid.UUID, error)
}

// Enroller is the enrollment use-case.
type Enroller struct {
	deps Deps
}

// Result is the persisted, non-secret outcome of a successful enrollment.
type Result struct {
	DeviceID    string
	TenantID    string
	ParentOrgID string // "" for a direct (non-MSP) tenant
	Region      string
	LocationID  string // "" when the server assigned none
}

// New validates dependencies and returns an Enroller.
func New(deps Deps) (*Enroller, error) {
	switch {
	case deps.Config == nil:
		return nil, errors.New("enroll: nil config")
	case deps.Identity == nil:
		return nil, errors.New("enroll: nil identity")
	case deps.Client == nil:
		return nil, errors.New("enroll: nil client")
	case deps.State == nil:
		return nil, errors.New("enroll: nil state")
	case deps.Audit == nil:
		return nil, errors.New("enroll: nil audit")
	}
	if deps.NewIdempotencyKey == nil {
		deps.NewIdempotencyKey = func() (uuid.UUID, error) { return uuid.NewRandom() }
	}
	return &Enroller{deps: deps}, nil
}

// Enroll runs one enrollment attempt: it ensures local identity, builds and sends
// the request with a stable Idempotency-Key, persists the server-authoritative
// credential, and emits audit events (attempt, then succeeded / failed /
// token_rejected). The returned error uses the typed sentinels above.
func (e *Enroller) Enroll(ctx context.Context) (*Result, error) {
	token := e.deps.Config.General.EnrollmentToken.Expose()
	if token == "" {
		return nil, ErrNoEnrollmentToken
	}

	// 1. Local-first identity material (device_guid, key, CSR, SPKI fp, hashed HW signals).
	mat, err := e.deps.Identity.Ensure()
	if err != nil {
		return nil, fmt.Errorf("enroll: identity: %w", err)
	}

	// 2. Stable Idempotency-Key for THIS attempt (one key per attempt; any
	//    transport-level retry reuses the same request, so the server can dedupe).
	key, err := e.deps.NewIdempotencyKey()
	if err != nil {
		return nil, fmt.Errorf("enroll: idempotency key: %w", err)
	}

	// 3. enroll.attempt — fail closed if the tamper-evident audit chain cannot
	//    record the attempt (nothing has been sent or persisted yet).
	if aerr := e.emit(audit.EventEnrollAttempt, audit.CategoryAuth, audit.SeverityInfo, audit.OutcomeSuccess, map[string]any{
		"device_guid":          mat.DeviceGUID,
		"idempotency_key":      key.String(),
		"advisory_location_id": e.deps.AdvisoryLocationID,
	}); aerr != nil {
		return nil, fmt.Errorf("%w: attempt: %v", ErrAudit, aerr)
	}
	e.logInfo("enrollment.attempt", "device_guid", mat.DeviceGUID)

	// 4. Build the request. The enrollment token is placed in the body only and
	//    is never logged.
	req, err := buildEnrollRequest(e.deps.Config, mat, token, e.deps.AdvisoryLocationID)
	if err != nil {
		aerr := e.emit(audit.EventEnrollFailed, audit.CategoryAuth, audit.SeverityWarn, audit.OutcomeFailure, map[string]any{"reason": "request_build"})
		e.logWarn("enrollment.failed", "reason", "request_build")
		return nil, errors.Join(fmt.Errorf("%w: building request: %v", ErrEnrollFailed, err), auditErr(aerr))
	}

	// 5. Send (Idempotency-Key supplied; server-side dedupe is NOT implemented here).
	resp, err := e.deps.Client.Enroll(ctx, req, saasclient.WithIdempotencyKey(key))
	if err != nil {
		return nil, e.handleTransportError(err)
	}

	// 6. Persist server-authoritative values (secrets via PutSecret).
	result, perr := e.persist(resp)
	if perr != nil {
		aerr := e.emit(audit.EventEnrollFailed, audit.CategoryAuth, audit.SeverityCritical, audit.OutcomeFailure, map[string]any{
			"reason":    "state_persist",
			"device_id": resp.DeviceId, // the server enrolled us, but local persistence failed
		})
		e.logError("enrollment.persist_failed", "device_id", resp.DeviceId)
		return nil, errors.Join(fmt.Errorf("%w: %v", ErrPersist, perr), auditErr(aerr))
	}

	// 7. enroll.succeeded. The enrollment is durable, so the Result is returned
	//    even if this audit emit fails — but the audit failure is surfaced, never
	//    hidden.
	aerr := e.emit(audit.EventEnrollSucceeded, audit.CategoryAuth, audit.SeverityInfo, audit.OutcomeSuccess, map[string]any{
		"device_id":     result.DeviceID,
		"tenant_id":     result.TenantID,
		"parent_org_id": result.ParentOrgID,
		"region":        result.Region,
		"location_id":   result.LocationID,
	})
	e.logInfo("enrollment.succeeded", "device_id", result.DeviceID)
	if aerr != nil {
		return result, fmt.Errorf("%w: succeeded: %v", ErrAudit, aerr)
	}
	return result, nil
}

// handleTransportError maps a saasclient error to a typed enrollment error and
// emits the matching audit event. The transport failure is always part of the
// returned error; any audit-emit failure is joined alongside it.
func (e *Enroller) handleTransportError(err error) error {
	switch {
	case errors.Is(err, saasclient.ErrUnauthorized):
		aerr := e.emit(eventEnrollTokenRejected, audit.CategoryAuth, audit.SeverityWarn, audit.OutcomeDenied, map[string]any{"reason": "unauthorized"})
		e.logWarn("enrollment.token_rejected", "reason", "unauthorized")
		return errors.Join(fmt.Errorf("%w: %v", ErrTokenRejected, err), auditErr(aerr))
	case errors.Is(err, saasclient.ErrConflict):
		aerr := e.emit(eventEnrollTokenRejected, audit.CategoryAuth, audit.SeverityWarn, audit.OutcomeDenied, map[string]any{"reason": "token_consumed"})
		e.logWarn("enrollment.token_rejected", "reason", "token_consumed")
		return errors.Join(fmt.Errorf("%w: %v", ErrTokenRejected, err), auditErr(aerr))
	case errors.Is(err, saasclient.ErrUpgradeRequired):
		aerr := e.emit(audit.EventEnrollFailed, audit.CategoryAuth, audit.SeverityWarn, audit.OutcomeFailure, map[string]any{"reason": "upgrade_required"})
		e.logWarn("enrollment.failed", "reason", "upgrade_required")
		return errors.Join(fmt.Errorf("%w: %v", ErrUpgradeRequired, err), auditErr(aerr))
	default:
		aerr := e.emit(audit.EventEnrollFailed, audit.CategoryAuth, audit.SeverityWarn, audit.OutcomeFailure, map[string]any{"reason": "transport"})
		e.logWarn("enrollment.failed", "reason", "transport")
		return errors.Join(fmt.Errorf("%w: %v", ErrEnrollFailed, err), auditErr(aerr))
	}
}

// persist writes the server-authoritative enrollment result. Non-secret binding
// values use Put; the session token and license blob use PutSecret. It stops at
// the first failure (a partial write is recoverable via a re-enrollment attempt).
func (e *Enroller) persist(resp *proto.EnrollResponse) (*Result, error) {
	if err := e.deps.State.Put(state.KeyDeviceID, []byte(resp.DeviceId)); err != nil {
		return nil, fmt.Errorf("device_id: %w", err)
	}
	if err := e.deps.State.Put(state.KeyTenantID, []byte(resp.TenantId)); err != nil {
		return nil, fmt.Errorf("tenant_id: %w", err)
	}
	parentOrg := ""
	if resp.ParentOrgId != nil {
		parentOrg = *resp.ParentOrgId
		if err := e.deps.State.Put(state.KeyParentOrgID, []byte(parentOrg)); err != nil {
			return nil, fmt.Errorf("parent_org_id: %w", err)
		}
	}
	if err := e.deps.State.Put(state.KeyRegion, []byte(resp.Region)); err != nil {
		return nil, fmt.Errorf("region: %w", err)
	}
	location := ""
	if resp.LocationId != nil {
		location = *resp.LocationId
		if err := e.deps.State.Put(state.KeyLocationID, []byte(location)); err != nil {
			return nil, fmt.Errorf("location_id: %w", err)
		}
	}
	if err := e.deps.State.Put(state.KeyCertificate, []byte(resp.AgentCertificatePem)); err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	// Secrets — wrapped at rest by the store; never written through Put.
	if err := e.deps.State.PutSecret(state.SecretSessionToken, []byte(resp.AgentSessionToken)); err != nil {
		return nil, fmt.Errorf("session_token: %w", err)
	}
	if resp.LicenseBlob != nil {
		blob, err := json.Marshal(resp.LicenseBlob)
		if err != nil {
			return nil, fmt.Errorf("license_blob marshal: %w", err)
		}
		if err := e.deps.State.PutSecret(state.SecretLicenseBlob, blob); err != nil {
			return nil, fmt.Errorf("license_blob: %w", err)
		}
	}
	return &Result{
		DeviceID:    resp.DeviceId,
		TenantID:    resp.TenantId,
		ParentOrgID: parentOrg,
		Region:      resp.Region,
		LocationID:  location,
	}, nil
}

// emit sends one audit event with the agent as actor and returns the emit error
// (if any) so the caller can surface it.
func (e *Enroller) emit(eventType, category, severity, outcome string, detail map[string]any) error {
	_, err := e.deps.Audit.Emit(audit.Event{
		EventType: eventType,
		Category:  category,
		Severity:  severity,
		Outcome:   outcome,
		Actor:     actorInstaller,
		Detail:    detail,
	})
	return err
}

// auditErr wraps an audit-emit error as ErrAudit, or returns nil. Used with
// errors.Join so an audit failure is reported but never hides the real outcome.
func auditErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrAudit, err)
}

// buildEnrollRequest assembles the EnrollRequest. tenant_id/region are advisory
// (server-authoritative); the token is set in the body only and never logged.
func buildEnrollRequest(cfg *config.Config, mat *identity.Material, token, advisoryLocation string) (proto.EnrollRequest, error) {
	guid, err := uuid.Parse(mat.DeviceGUID)
	if err != nil {
		return proto.EnrollRequest{}, fmt.Errorf("invalid device guid %q: %w", mat.DeviceGUID, err)
	}
	et := proto.EnrollmentToken(token)
	spki := mat.SPKIFingerprint
	av := proto.AgentVersion(wireversion.AgentVersion())
	req := proto.EnrollRequest{
		EnrollmentToken: &et,
		DeviceGuid:      guid,
		CsrPem:          string(mat.CSRPEM),
		SpkiSha256:      &spki,
		RecoveryPolicy:  proto.Escrowed, // Sprint-1 default (ADR-001); zero_knowledge lands in Sprint 4
		AgentVersion:    &av,
		Fingerprint:     hardwareSignals(mat.HardwareSignals),
	}
	if v := cfg.General.TenantID; v != "" {
		t := proto.TenantId(v)
		req.TenantId = &t
	}
	if v := cfg.General.Region; v != "" {
		r := proto.Region(v)
		req.Region = &r
	}
	if advisoryLocation != "" {
		l := proto.LocationId(advisoryLocation)
		req.LocationId = &l
	}
	return req, nil
}

// hardwareSignals maps the already-hashed identity signals to the proto type.
// Raw identifying values are never present here (they are hashed at collection),
// and nothing in this struct is persisted in agent state.
func hardwareSignals(hs identity.HardwareSignals) *proto.HardwareSignals {
	out := &proto.HardwareSignals{}
	set := false
	if hs.MachineGUIDSHA256 != "" {
		v := hs.MachineGUIDSHA256
		out.MachineGuidSha256 = &v
		set = true
	}
	if hs.PrimaryDiskSerialSHA256 != "" {
		v := hs.PrimaryDiskSerialSHA256
		out.PrimaryDiskSerialSha256 = &v
		set = true
	}
	if hs.FirstNICMACSHA256 != "" {
		v := hs.FirstNICMACSHA256
		out.FirstNicMacSha256 = &v
		set = true
	}
	if hs.OS != "" {
		o := proto.HardwareSignalsOs(hs.OS)
		out.Os = &o
		set = true
	}
	if hs.OSVersion != "" {
		v := hs.OSVersion
		out.OsVersion = &v
		set = true
	}
	if !set {
		return nil
	}
	return out
}

// ---- nil-safe operational logging (never receives secrets) ------------------

func (e *Enroller) logInfo(event string, kv ...string) {
	if e.deps.Log == nil {
		return
	}
	logEvent(e.deps.Log.Info(event), kv)
}

func (e *Enroller) logWarn(event string, kv ...string) {
	if e.deps.Log == nil {
		return
	}
	logEvent(e.deps.Log.Warn(event), kv)
}

func (e *Enroller) logError(event string, kv ...string) {
	if e.deps.Log == nil {
		return
	}
	logEvent(e.deps.Log.Error(event), kv)
}

// logEvent appends string key/value pairs and sends the event. Callers pass only
// non-secret values; the logging package additionally redacts tokens at the sink.
func logEvent(ev *zerolog.Event, kv []string) {
	for i := 0; i+1 < len(kv); i += 2 {
		ev = ev.Str(kv[i], kv[i+1])
	}
	ev.Msg("")
}
