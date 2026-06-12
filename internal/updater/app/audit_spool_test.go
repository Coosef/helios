package app

import (
	"io"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
)

// The updater writes to its OWN audit spool (its own appender), with Source=updater,
// and the additive update.failed event must be accepted by the frozen vocabulary.
func TestUpdaterAuditSpoolSeparateAndAccepts(t *testing.T) {
	if !audit.IsValidEventType("update.failed") {
		t.Fatal("update.failed must be in the controlled vocabulary")
	}
	spool := audit.NewMemoryAppender() // the updater's OWN spool (not the agent's)
	em, err := audit.New(spool, io.Discard, audit.Identity{Source: audit.SourceUpdater, DeviceGUID: "dev-1"})
	if err != nil {
		t.Fatalf("audit.New with Source=updater must succeed: %v", err)
	}
	a := ProdAuditor{Emitter: em}
	a.Emit("update.failed", audit.OutcomeFailure, map[string]any{"reason": "test"})
	a.Emit(audit.EventUpdateRolledBack, audit.OutcomeSuccess, nil)

	recs, err := spool.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("own spool got %d records, want 2", len(recs))
	}
	if recs[0].Source != audit.SourceUpdater || recs[0].EventType != "update.failed" {
		t.Errorf("record[0] = %+v; want updater/update.failed", recs[0])
	}
	// chain links within the updater's own spool
	if recs[1].PrevHash != recs[0].ThisHash {
		t.Error("updater spool chain not linked")
	}
}

func TestSourceUpdaterAcceptedAgentStillValid(t *testing.T) {
	if _, err := audit.New(audit.NewMemoryAppender(), io.Discard, audit.Identity{Source: audit.SourceAgent}); err != nil {
		t.Errorf("agent source still valid: %v", err)
	}
	if _, err := audit.New(audit.NewMemoryAppender(), io.Discard, audit.Identity{Source: "bogus"}); err == nil {
		t.Error("an unknown source must still be rejected")
	}
}
