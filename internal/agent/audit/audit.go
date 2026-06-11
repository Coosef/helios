// Package audit emits the agent's tamper-evident security-event records.
//
// Each record is part of a per-device hash chain: it carries the previous
// record's hash (prev_hash) and its own hash, computed as
//
//	this_hash = BLAKE3( JCS(record without this_hash & signature & server fields) )
//
// where JCS is RFC 8785 JSON Canonicalization (byte-reproducible). The chain is
// anchored to a device-bound genesis hash so it cannot be grafted onto another
// device. Records are appended to a pluggable Appender (in-memory or JSONL here;
// the bbolt-backed store lands in S1-T10) and mirrored to a dedicated audit
// stream.
//
// SCOPE (S1-T09, per the approved audit design review): schema, canonicalization,
// local BLAKE3 chain, local verification, controlled vocabulary, dedicated
// stream. The signature and server-anchoring fields are RESERVED (always present
// as null) and are EXCLUDED from the hash, so they can be filled in later without
// invalidating it; signing (S1-T14), server anchoring / WORM, TPM keys, and
// retention enforcement are Sprint 8+.
//
// IMPORTANT (threat model, per §0.4): a local hash chain is tamper-EVIDENT
// against accidental corruption and non-privileged tampering only. A SYSTEM/admin
// attacker can recompute the chain; true tamper-evidence requires off-device WORM
// anchoring (Sprint 8). Do not over-claim.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gowebpki/jcs"

	"github.com/beyzbackup/beyz-backup/pkg/hashing"
)

// SchemaVersion is the frozen audit-record schema version.
const SchemaVersion = 1

const genesisSalt = "|beyz-audit-genesis-v1"

// Source identifies the producing subsystem. "agent" is the agent process;
// "updater" is the on-demand updater (S1-T27), which writes its OWN audit spool
// (never the agent's hash chain). "server" and "restore-engine" are reserved.
const (
	SourceAgent   = "agent"
	SourceUpdater = "updater"
)

// Category values.
const (
	CategoryAuth      = "auth"
	CategoryAccess    = "access"
	CategoryConfig    = "config"
	CategoryUpdate    = "update"
	CategoryLifecycle = "lifecycle"
	CategoryLicense   = "license"
	CategoryRestore   = "restore"
	CategoryIntegrity = "integrity"
)

// Severity values.
const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityCritical = "critical"
)

// Outcome values.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// A selection of common event types (the full controlled vocabulary is in
// validEventTypes).
const (
	EventEnrollAttempt    = "enroll.attempt"
	EventEnrollSucceeded  = "enroll.succeeded"
	EventEnrollFailed     = "enroll.failed"
	EventAuthFailure      = "auth.failure"
	EventConfigTamper     = "config.tamper_detected"
	EventServiceStarted   = "service.started"
	EventServiceStopped   = "service.stopped"
	EventUpdateRolledBack = "update.rolled_back"
	EventUpdateFailed     = "update.failed"
)

// Sentinel errors. Match with errors.Is.
var (
	ErrUnknownEventType = errors.New("audit: unknown event_type")
	ErrInvalidField     = errors.New("audit: invalid field value")
	ErrChainGap         = errors.New("audit: sequence gap")
	ErrChainBroken      = errors.New("audit: prev_hash does not link")
	ErrTampered         = errors.New("audit: record hash mismatch (tampered)")
)

// Target is the resource an event acted on (PCI 10.3 "affected resource").
type Target struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Signature is the agent's signature over this_hash. RESERVED in Sprint 1 (always
// null); wired post-enrollment (S1-T14).
type Signature struct {
	Alg   string `json:"alg"`
	KeyID string `json:"key_id"`
	Value string `json:"value"`
}

// Record is one frozen, hash-chained audit record. All fields are always present
// (nullable fields serialize as null) so the schema and the hash are well-defined.
type Record struct {
	SchemaVersion int     `json:"schema_version"`
	Seq           uint64  `json:"seq"`
	PrevHash      string  `json:"prev_hash"`
	ThisHash      string  `json:"this_hash"`     // excluded from the hash
	TSLocal       string  `json:"ts_local"`      // device clock (RFC3339)
	TSServer      *string `json:"ts_server"`     // RESERVED, server-filled; excluded from the hash
	ClockSkewMS   *int    `json:"clock_skew_ms"` // device vs server (GAP-5)
	Source        string  `json:"source"`        // "agent"
	EventType     string  `json:"event_type"`    // controlled vocabulary
	Category      string  `json:"category"`
	Severity      string  `json:"severity"`
	Outcome       string  `json:"outcome"`
	Actor         string  `json:"actor"` // system|installer|admin:<id>|user:<id>
	TenantID      string  `json:"tenant_id"`
	ParentOrgID   *string `json:"parent_org_id"` // MSP hierarchy
	DeviceID      string  `json:"device_id"`
	AgentVersion  string  `json:"agent_version"`
	Target        *Target `json:"target"`
	TraceID       string  `json:"trace_id"`

	Detail            map[string]any `json:"detail"`              // redacted, non-sensitive
	Signature         *Signature     `json:"signature"`           // RESERVED; excluded from the hash
	ServerAnchorProof *string        `json:"server_anchor_proof"` // RESERVED; excluded from the hash
}

// hashExcludedFields are not covered by this_hash: this_hash itself, and the
// fields that are filled AFTER the record is created/hashed (server side or by
// the later signing step).
var hashExcludedFields = []string{"this_hash", "signature", "ts_server", "server_anchor_proof"}

// canonicalRecordJSON returns the RFC 8785 (JCS) canonical serialization of the
// record with the hash-excluded fields removed.
func canonicalRecordJSON(r Record) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("audit: marshaling record: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("audit: re-decoding record: %w", err)
	}
	for _, f := range hashExcludedFields {
		delete(m, f)
	}
	canonInput, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("audit: marshaling canonical input: %w", err)
	}
	canon, err := jcs.Transform(canonInput)
	if err != nil {
		return nil, fmt.Errorf("audit: JCS canonicalization: %w", err)
	}
	return canon, nil
}

// computeThisHash returns the tagged BLAKE3 digest of the canonical record.
func computeThisHash(r Record) (string, error) {
	canon, err := canonicalRecordJSON(r)
	if err != nil {
		return "", err
	}
	d, err := hashing.HashBytes(hashing.BLAKE3, canon)
	if err != nil {
		return "", fmt.Errorf("audit: hashing record: %w", err)
	}
	return d.String(), nil
}

// GenesisPrevHash returns the device-bound genesis prev_hash that anchors a
// device's chain to its identity (anti-grafting).
func GenesisPrevHash(deviceGUID string) string {
	// BLAKE3 is always registered, so HashBytes cannot fail here.
	d, _ := hashing.HashBytes(hashing.BLAKE3, []byte(deviceGUID+genesisSalt))
	return d.String()
}

// validEventTypes is the frozen controlled vocabulary (design §5).
var validEventTypes = newSet(
	"enroll.attempt", "enroll.succeeded", "enroll.failed", "enroll.token_rejected",
	"auth.failure", "cert.renewed", "cert.rejected", "spki_pin.mismatch",
	"update.offered", "update.manifest_verified", "update.signature_invalid",
	"update.hash_mismatch", "update.staged", "update.swapped", "update.health_ok",
	"update.rolled_back", "update.downgrade_blocked", "update.started",
	"update.succeeded", "update.signature_failed", "update.failed",
	"integrity.check_failed",
	"config.tamper_detected", "config.reloaded",
	"license.issued", "license.renewed", "license.revoked", "license.signature_invalid",
	"license.over_quota",
	"restore.started", "restore.integrity_result", "restore.completed", "restore.quarantined",
	"service.started", "service.stopped", "decommission.started",
)

var (
	validCategories = newSet(CategoryAuth, CategoryAccess, CategoryConfig, CategoryUpdate,
		CategoryLifecycle, CategoryLicense, CategoryRestore, CategoryIntegrity)
	validSeverities = newSet(SeverityInfo, SeverityWarn, SeverityCritical)
	validOutcomes   = newSet(OutcomeSuccess, OutcomeFailure, OutcomeDenied)
	validSources    = newSet(SourceAgent, SourceUpdater)
)

func newSet(items ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, it := range items {
		m[it] = struct{}{}
	}
	return m
}

// IsValidEventType reports whether eventType is in the controlled vocabulary.
func IsValidEventType(eventType string) bool {
	_, ok := validEventTypes[eventType]
	return ok
}
