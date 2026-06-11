package app

import (
	"context"
	"errors"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/agent/service"
	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// This file holds the production implementations of the orchestrator's collaborator
// interfaces. Tests inject fakes instead; cmd/updater wires these.

// ProdChecker performs the T24 fetch+verify+decide over the SPKI-pinned control
// channel (the manifest), using the embedded Ed25519 key set.
type ProdChecker struct {
	Client   *httpclient.Client
	URL      string
	MaxBytes int64
	Keys     *trust.KeySet
	Platform string
	Arch     string
}

func (c *ProdChecker) Check(ctx context.Context, baseline manifest.Version) (*manifestcheck.Decision, error) {
	return manifestcheck.Check(ctx, c.Client, c.URL, c.MaxBytes, c.Keys, baseline, c.Platform, c.Arch)
}

// ProdService controls the installed agent service BY NAME (T18 name-only helpers).
type ProdService struct{ Name string }

func (s ProdService) Stop() error            { return service.ControlService(s.Name, "stop") }
func (s ProdService) Start() error           { return service.ControlService(s.Name, "start") }
func (s ProdService) Running() (bool, error) { return service.StatusService(s.Name) }

// ProdMarker implements the T26 marker/health handshake against the state dir.
type ProdMarker struct{ StateDir string }

func (m ProdMarker) Write(updateID string) error {
	return health.WriteMarker(m.StateDir, health.Marker{UpdateID: updateID})
}

// Clear removes BOTH the marker and the health record after every commit/rollback.
// Both removals are attempted even if one fails (no asymmetric leftover state); the
// errors are joined.
func (m ProdMarker) Clear() error {
	return errors.Join(health.RemoveMarker(m.StateDir), health.RemoveHealth(m.StateDir))
}

// ProdAuditor writes to the updater's OWN hash-chained audit spool (Source=updater),
// never the agent's. Emission is best-effort: an audit-write failure is logged but
// never blocks the update (the update is gated by cryptography, not by audit).
type ProdAuditor struct {
	Emitter *audit.Emitter
	Log     *logging.Logger
}

func (a ProdAuditor) Emit(eventType, outcome string, detail map[string]any) {
	if a.Emitter == nil {
		return
	}
	if _, err := a.Emitter.Emit(audit.Event{
		EventType: eventType,
		Category:  audit.CategoryUpdate,
		Severity:  severityFor(eventType),
		Outcome:   outcome,
		Actor:     "system",
		Detail:    detail,
	}); err != nil && a.Log != nil {
		a.Log.Warn("updater.audit_emit_failed").Str("event", eventType).Str("err", err.Error()).Msg("")
	}
}

func severityFor(eventType string) string {
	switch eventType {
	case evSignatureInvalid, evRolledBack:
		return audit.SeverityCritical
	case evFailed, evDowngradeBlocked, evHashMismatch:
		return audit.SeverityWarn
	default:
		return audit.SeverityInfo
	}
}
