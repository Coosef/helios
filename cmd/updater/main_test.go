package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/app"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

type fakeRunner struct {
	dec      *manifestcheck.Decision
	checkErr error
	out      app.Outcome
	applyErr error
}

func (f fakeRunner) Check(context.Context) (*manifestcheck.Decision, error) { return f.dec, f.checkErr }
func (f fakeRunner) Apply(context.Context) (app.Outcome, error)             { return f.out, f.applyErr }

func withRunner(r runner, fn func()) {
	prev := buildRunner
	buildRunner = func(bootstrapOptions) (runner, error) { return r, nil }
	defer func() { buildRunner = prev }()
	fn()
}

func runCLI(t *testing.T, r runner, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	var code int
	withRunner(r, func() { code = run(args, &out, &errb) })
	return code, out.String(), errb.String()
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"--version"}, &out, &out)
	if code != exitOK || !strings.Contains(out.String(), "beyz-backup-updater") {
		t.Errorf("--version: code=%d out=%q", code, out.String())
	}
}

func TestNoSubcommandErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != exitError {
		t.Errorf("no subcommand: code=%d, want %d", code, exitError)
	}
	if code := run([]string{"frobnicate"}, &out, &errb); code != exitError {
		t.Errorf("unknown subcommand: code=%d", code)
	}
}

func proceedDecision() *manifestcheck.Decision {
	cv, _ := manifest.ParseVersion("1.1.0")
	tv, _ := manifest.ParseVersion("1.2.0")
	return &manifestcheck.Decision{Proceed: true, CurrentVersion: cv, TargetVersion: tv}
}

func TestCheckProceedAndReject(t *testing.T) {
	code, out, _ := runCLI(t, fakeRunner{dec: proceedDecision()}, "check")
	if code != exitOK || !strings.Contains(out, "update available") {
		t.Errorf("check proceed: code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, fakeRunner{dec: &manifestcheck.Decision{Proceed: false, Reason: "downgrade_blocked"}}, "check")
	if code != exitOK || !strings.Contains(out, "no update") {
		t.Errorf("check reject: code=%d out=%q", code, out)
	}
	code, _, _ = runCLI(t, fakeRunner{dec: nil, checkErr: context.Canceled}, "check")
	if code != exitError {
		t.Errorf("check nil decision: code=%d, want %d", code, exitError)
	}
}

func TestApplyOutcomeExitCodes(t *testing.T) {
	cases := map[app.Outcome]int{
		app.OutcomeUpdated:        exitOK,
		app.OutcomeNoUpdate:       exitOK,
		app.OutcomeRecovered:      exitOK,
		app.OutcomeRejected:       exitRejected,
		app.OutcomeRolledBack:     exitRolledBack,
		app.OutcomeRollbackFailed: exitRollbackFailed,
		app.OutcomeError:          exitError,
	}
	for out, want := range cases {
		code, stdout, _ := runCLI(t, fakeRunner{out: out}, "apply")
		if code != want {
			t.Errorf("apply %s: code=%d, want %d", out, code, want)
		}
		if !strings.Contains(stdout, string(out)) {
			t.Errorf("apply %s: stdout missing outcome: %q", out, stdout)
		}
	}
}

func TestExitCodeForMapping(t *testing.T) {
	if exitCodeFor(app.OutcomeRollbackFailed) != exitRollbackFailed {
		t.Error("rollback_failed must map to its distinct code")
	}
	if exitCodeFor(app.OutcomeUpdated) != exitOK {
		t.Error("updated -> ok")
	}
}
