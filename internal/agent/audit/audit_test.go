package audit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
)

func TestFileAppenderCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.jsonl")
	if err := os.WriteFile(path, []byte("{not valid json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fa := audit.NewFileAppender(path)
	if _, err := fa.Records(); err == nil {
		t.Error("Records(corrupt) should error")
	}
	// New must surface the chain-head read error.
	if _, err := audit.New(fa, nil, testID()); err == nil {
		t.Error("New over a corrupt audit file should error")
	}
}

const guid = "guid-abc-123"

func testID() audit.Identity {
	return audit.Identity{
		TenantID:     "tnt_1",
		ParentOrgID:  "msp_1",
		DeviceID:     "dev_1",
		DeviceGUID:   guid,
		AgentVersion: "1.0.0",
		Source:       audit.SourceAgent,
	}
}

func emit(t *testing.T, e *audit.Emitter, eventType string) audit.Record {
	t.Helper()
	r, err := e.Emit(audit.Event{
		EventType: eventType,
		Category:  audit.CategoryAuth,
		Severity:  audit.SeverityInfo,
		Outcome:   audit.OutcomeSuccess,
		Actor:     "system",
	})
	if err != nil {
		t.Fatalf("Emit(%s): %v", eventType, err)
	}
	return r
}

func TestEmitChainAndVerify(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, err := audit.New(app, nil, testID())
	if err != nil {
		t.Fatal(err)
	}
	r1 := emit(t, e, "enroll.attempt")
	r2 := emit(t, e, "enroll.succeeded")
	r3 := emit(t, e, "service.started")

	if r1.Seq != 1 || r2.Seq != 2 || r3.Seq != 3 {
		t.Errorf("seq wrong: %d %d %d", r1.Seq, r2.Seq, r3.Seq)
	}
	if r1.PrevHash != audit.GenesisPrevHash(guid) {
		t.Errorf("first prev_hash must be the device genesis: %q", r1.PrevHash)
	}
	if r2.PrevHash != r1.ThisHash || r3.PrevHash != r2.ThisHash {
		t.Error("prev_hash must link to the previous this_hash")
	}
	if !strings.HasPrefix(r1.ThisHash, "blake3:") {
		t.Errorf("this_hash must be a tagged BLAKE3 digest: %q", r1.ThisHash)
	}
	if r1.TenantID != "tnt_1" || r1.ParentOrgID == nil || *r1.ParentOrgID != "msp_1" || r1.Source != "agent" {
		t.Errorf("identity fields wrong: %+v", r1)
	}

	recs, _ := app.Records()
	if err := audit.Verify(recs, guid); err != nil {
		t.Fatalf("Verify(good chain): %v", err)
	}
}

func TestMemoryAppenderResume(t *testing.T) {
	app := audit.NewMemoryAppender()
	e1, _ := audit.New(app, nil, testID())
	emit(t, e1, "enroll.attempt")
	emit(t, e1, "service.started")

	// A second emitter over the same in-memory store resumes the chain.
	e2, err := audit.New(app, nil, testID())
	if err != nil {
		t.Fatal(err)
	}
	if seq, _ := e2.Head(); seq != 2 {
		t.Fatalf("resumed head seq = %d, want 2", seq)
	}
	if r := emit(t, e2, "service.stopped"); r.Seq != 3 {
		t.Errorf("post-resume seq = %d, want 3", r.Seq)
	}
	recs, _ := app.Records()
	if err := audit.Verify(recs, guid); err != nil {
		t.Errorf("Verify(resumed in-memory chain): %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, nil, testID())
	emit(t, e, "enroll.attempt")
	emit(t, e, "auth.failure")
	emit(t, e, "service.started")
	recs, _ := app.Records()

	// Tamper the content of a record.
	recs[1].Actor = "attacker"
	if err := audit.Verify(recs, guid); !errors.Is(err, audit.ErrTampered) {
		t.Errorf("content tamper: err = %v, want ErrTampered", err)
	}

	// Tamper the stored hash.
	recs, _ = app.Records()
	recs[0].ThisHash = "blake3:deadbeef"
	if err := audit.Verify(recs, guid); !errors.Is(err, audit.ErrTampered) {
		t.Errorf("hash tamper: err = %v, want ErrTampered", err)
	}
}

func TestVerifyDetectsRemovalAndReorder(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, nil, testID())
	emit(t, e, "enroll.attempt")
	emit(t, e, "enroll.succeeded")
	emit(t, e, "service.started")
	recs, _ := app.Records()

	// Remove the middle record -> sequence gap.
	removed := []audit.Record{recs[0], recs[2]}
	if err := audit.Verify(removed, guid); !errors.Is(err, audit.ErrChainGap) {
		t.Errorf("removal: err = %v, want ErrChainGap", err)
	}

	// Reorder -> sequence gap.
	reordered := []audit.Record{recs[1], recs[0], recs[2]}
	if err := audit.Verify(reordered, guid); !errors.Is(err, audit.ErrChainGap) {
		t.Errorf("reorder: err = %v, want ErrChainGap", err)
	}
}

func TestVerifyRejectsUnknownEventTypeInRecord(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, nil, testID())
	emit(t, e, "enroll.attempt")
	recs, _ := app.Records()

	recs[0].EventType = "smuggled.event"
	if err := audit.Verify(recs, guid); !errors.Is(err, audit.ErrUnknownEventType) {
		t.Errorf("verify with bad event_type: err = %v, want ErrUnknownEventType", err)
	}
}

func TestWithClockDeterministicTimestamp(t *testing.T) {
	fixed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	e, _ := audit.New(audit.NewMemoryAppender(), nil, testID(), audit.WithClock(func() time.Time { return fixed }))
	r := emit(t, e, "service.started")
	if r.TSLocal != "2026-06-09T12:00:00Z" {
		t.Errorf("ts_local = %q, want the injected clock value", r.TSLocal)
	}
}

func TestVerifyDetectsGrafting(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, nil, testID())
	emit(t, e, "enroll.attempt")
	recs, _ := app.Records()

	// Verifying against a different device's genesis must fail (anti-grafting).
	if err := audit.Verify(recs, "some-other-device"); !errors.Is(err, audit.ErrChainBroken) {
		t.Errorf("grafting: err = %v, want ErrChainBroken", err)
	}
}

func TestEmptyChainVerifies(t *testing.T) {
	if err := audit.Verify(nil, guid); err != nil {
		t.Errorf("empty chain: %v", err)
	}
}

func TestControlledVocabularyEnforced(t *testing.T) {
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, nil, testID())

	if _, err := e.Emit(audit.Event{EventType: "totally.bogus", Category: audit.CategoryAuth, Severity: audit.SeverityInfo, Outcome: audit.OutcomeSuccess}); !errors.Is(err, audit.ErrUnknownEventType) {
		t.Errorf("bad event_type: err = %v, want ErrUnknownEventType", err)
	}
	for name, ev := range map[string]audit.Event{
		"bad category": {EventType: "auth.failure", Category: "nope", Severity: audit.SeverityInfo, Outcome: audit.OutcomeSuccess},
		"bad severity": {EventType: "auth.failure", Category: audit.CategoryAuth, Severity: "nope", Outcome: audit.OutcomeSuccess},
		"bad outcome":  {EventType: "auth.failure", Category: audit.CategoryAuth, Severity: audit.SeverityInfo, Outcome: "nope"},
	} {
		if _, err := e.Emit(ev); !errors.Is(err, audit.ErrInvalidField) {
			t.Errorf("%s: err = %v, want ErrInvalidField", name, err)
		}
	}
	if !audit.IsValidEventType("auth.failure") || audit.IsValidEventType("auth.nope") {
		t.Error("IsValidEventType wrong")
	}
}

func TestSourceValidationAndDefault(t *testing.T) {
	if _, err := audit.New(audit.NewMemoryAppender(), nil, audit.Identity{Source: "hacker", DeviceGUID: guid}); !errors.Is(err, audit.ErrInvalidField) {
		t.Errorf("bad source: err = %v, want ErrInvalidField", err)
	}
	if _, err := audit.New(nil, nil, testID()); !errors.Is(err, audit.ErrInvalidField) {
		t.Errorf("nil appender: err = %v, want ErrInvalidField", err)
	}
	// Empty source defaults to "agent".
	e, err := audit.New(audit.NewMemoryAppender(), nil, audit.Identity{DeviceGUID: guid})
	if err != nil {
		t.Fatal(err)
	}
	if r := emit(t, e, "service.started"); r.Source != audit.SourceAgent {
		t.Errorf("default source = %q, want agent", r.Source)
	}
}

func TestDetailRedactionAndStream(t *testing.T) {
	var buf bytes.Buffer
	app := audit.NewMemoryAppender()
	e, _ := audit.New(app, &buf, testID())

	r, err := e.Emit(audit.Event{
		EventType: "auth.failure", Category: audit.CategoryAuth, Severity: audit.SeverityWarn, Outcome: audit.OutcomeFailure, Actor: "system",
		Detail: map[string]any{"header": "Bearer ast_sessionSECRET", "note": "leaked bzt_enrollSECRET", "code": 401},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Stored detail is redacted.
	if r.Detail["header"] != "Bearer ***REDACTED***" || r.Detail["note"] != "leaked bzt_***REDACTED***" {
		t.Errorf("detail not redacted: %v", r.Detail)
	}
	if r.Detail["code"] != 401 {
		t.Errorf("non-string detail mangled: %v", r.Detail["code"])
	}
	// Stream output is redacted and valid JSON.
	out := buf.String()
	for _, raw := range []string{"ast_sessionSECRET", "bzt_enrollSECRET"} {
		if strings.Contains(out, raw) {
			t.Errorf("raw secret %q leaked to audit stream: %s", raw, out)
		}
	}
	if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Errorf("audit stream line is not valid JSON: %s", out)
	}
}

func TestFileAppenderPersistAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	fa := audit.NewFileAppender(path)
	e1, err := audit.New(fa, nil, testID())
	if err != nil {
		t.Fatal(err)
	}
	emit(t, e1, "enroll.attempt")
	emit(t, e1, "enroll.succeeded")

	// A new emitter over the same file resumes the chain.
	fa2 := audit.NewFileAppender(path)
	e2, err := audit.New(fa2, nil, testID())
	if err != nil {
		t.Fatal(err)
	}
	if seq, _ := e2.Head(); seq != 2 {
		t.Fatalf("resumed head seq = %d, want 2", seq)
	}
	r3 := emit(t, e2, "service.started")
	if r3.Seq != 3 {
		t.Errorf("post-resume seq = %d, want 3", r3.Seq)
	}

	recs, err := fa2.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records = %d, want 3", len(recs))
	}
	if err := audit.Verify(recs, guid); err != nil {
		t.Errorf("Verify(resumed chain): %v", err)
	}
}
