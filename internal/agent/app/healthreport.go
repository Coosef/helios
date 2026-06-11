package app

import (
	"errors"
	"sync"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
	"github.com/beyzbackup/beyz-backup/internal/health"
	"github.com/beyzbackup/beyz-backup/pkg/proto"
)

// postUpdateReporter is the agent side of the S1-T26 post-update health handshake.
// On a post-update boot — detected by the presence of the updater-written update
// marker (internal/health) in the state directory — it (a) attaches
// update_result="ok" to heartbeats (the SaaS-facing UPD-4 signal) and (b) writes the
// local health record (state\health.json) echoing the marker's update_id after the
// first SUCCESSFUL heartbeat, so the updater's health gate can confirm the new agent
// is alive AND reachable. On a normal boot (no marker) it is inert.
//
// The marker's update_id is the freshness anchor: a stale health.json from a prior
// update carries a different update_id and cannot satisfy a later gate.
type postUpdateReporter struct {
	stateDir string
	log      *logging.Logger
	now      func() time.Time

	mu       sync.Mutex
	active   bool   // a post-update confirmation is pending
	updateID string // echoed from the marker
	written  bool   // health record already written for this update
}

// newPostUpdateReporter reads the update marker once at startup. If present and
// valid, the reporter is armed for THIS update_id; otherwise it stays inert.
//
// Reading once per process is correct by the update lifecycle: the updater STOPS
// the agent before swapping the binary and STARTS a fresh agent process afterward
// (STOPPING_AGENT → SWAPPING → STARTING_AGENT), so each update is observed by a
// brand-new reporter that reads the new marker. A single long-lived process never
// receives a new marker. The marker/health files are cleaned up by the updater
// (T27) after the gate resolves — not by the agent.
func newPostUpdateReporter(stateDir string, log *logging.Logger) *postUpdateReporter {
	r := &postUpdateReporter{stateDir: stateDir, log: log, now: time.Now}
	m, err := health.ReadMarker(stateDir)
	switch {
	case err == nil:
		r.active = true
		r.updateID = m.UpdateID
		if log != nil {
			log.Info("health.post_update_boot").Str("update_id", m.UpdateID).Msg("")
		}
	case !errors.Is(err, health.ErrAbsent):
		// A present-but-corrupt marker leaves the reporter inert (the gate will then
		// time out and roll back, fail-closed) — log it so the corruption is visible.
		if log != nil {
			log.Warn("health.marker_unreadable").Str("err", err.Error()).Msg("")
		}
	}
	return r
}

// report returns the update_result for the next heartbeat: "ok" while a post-update
// confirmation is still pending, nil otherwise. Wired to heartbeat.Deps.UpdateReport.
func (r *postUpdateReporter) report() *proto.UpdateResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.written {
		return nil
	}
	ok := proto.UpdateResultOk
	return &ok
}

// onBeatSuccess writes the health record once, after the first successful post-update
// heartbeat. A write failure is logged and retried on the next successful beat (the
// record is the rollback-avoidance signal, so it must never be faked). Wired to
// heartbeat.Deps.OnBeatSuccess.
func (r *postUpdateReporter) onBeatSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.written {
		return
	}
	rec := health.Record{
		UpdateID:  r.updateID,
		Result:    health.ResultOK,
		WrittenAt: r.now().UTC().Format(time.RFC3339),
	}
	if err := health.WriteHealth(r.stateDir, rec); err != nil {
		if r.log != nil {
			r.log.Warn("health.write_failed").Str("update_id", r.updateID).Str("err", err.Error()).Msg("")
		}
		return
	}
	r.written = true
	if r.log != nil {
		r.log.Info("health.reported").Str("update_id", r.updateID).Msg("")
	}
}
