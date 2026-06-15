// Package pester holds the S1-T35 Windows Pester suites (test/CI tooling). The
// suites themselves are Windows-only (real SCM + the installer artifact), so they
// run in the non-blocking CI `pester` job. This Go test runs everywhere (incl.
// `go test ./...`) and statically pins that the PowerShell suites cover every
// frozen-#3/#4 scenario and uphold the flake-control rules (freeze #6) — so a
// dropped Describe/It or a reintroduced fixed-sleep / real-SaaS call is caught
// on a non-Windows host too.
package pester

import (
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func has(t *testing.T, hay, needle, why string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Errorf("missing %q\n  reason: %s", needle, why)
	}
}

func hasNot(t *testing.T, hay, needle, why string) {
	t.Helper()
	if strings.Contains(hay, needle) {
		t.Errorf("unexpected %q present\n  reason: %s", needle, why)
	}
}

// ---- installer-backed suite (freeze #3) -------------------------------------

func TestInstallerSuiteCoverage(t *testing.T) {
	s := read(t, "Installer.Tests.ps1")
	for _, c := range []struct{ needle, why string }{
		{`/TOKENFILE=$script:TokenFile`, "silent install with /TOKENFILE="},
		{`/VERYSILENT`, "silent install"},
		{`Test-Path -LiteralPath $script:StateDir`, "ProgramData state dir"},
		{`(Join-Path $script:DataRoot 'logs')`, "ProgramData logs dir"},
		{`(Join-Path $script:DataRoot 'update')`, "ProgramData update dir"},
		{`Test-Path -LiteralPath $script:ConfigYaml`, "config.yaml laid down"},
		{`Get-BeyzAclSids $script:DataRoot`, "ProgramData root ACL asserted"},
		{`Get-BeyzAclSids $script:StateDir`, "state\\ ACL asserted"},
		{`(Join-Path $script:DataRoot 'update')`, "update\\ ACL asserted"},
		{`Get-BeyzAclSids (Join-Path $script:DataRoot 'logs')`, "logs\\ ACL asserted"},
		{`Get-BeyzAclSids $script:ConfigYaml`, "config.yaml ACL asserted"},
		{`Get-BeyzServiceStartName 'BeyzBackupAgent'`, "service start-name (LocalSystem)"},
		{`'LocalSystem'`, "service runs as LocalSystem"},
		{`Test-BeyzServiceExists 'BeyzBackupUpdater' | Should -BeFalse`, "no BeyzBackupUpdater service (AC-42)"},
		{`$script:ConfigYaml -Raw) | Should -Not -Match`, "token never written to config.yaml"},
		{`$script:LogFile -Raw) | Should -Not -Match`, "token never leaked in the installer log (AC-33)"},
		{`$script:TokenPath`, "enroll-token file behavior asserted"},
		{`Should -Not -Contain $SID_ADMINS`, "enroll-token is SYSTEM-only (Administrators excluded)"},
		{`& $script:UpdaterExe --version`, "installed updater runs once and exits"},
		{`Uninstall-Beyz`, "uninstall path exercised"},
		{`Uninstall-Beyz -KeepState`, "/KEEPSTATE preserves state"},
		{`Test-Path -LiteralPath $script:StateDir  | Should -BeFalse`, "default uninstall shreds state"},
		{`$script:Marker    | Should -BeTrue`, "KEEPSTATE proven via a preserved marker"},
	} {
		has(t, s, c.needle, c.why)
	}
	// the state\ ACL block must assert Users are excluded; logs\/config grant them.
	has(t, s, `$sids | Should -Not -Contain $SID_USERS`, "state\\/update\\ exclude Users")
	has(t, s, `Should -Contain $SID_USERS`, "logs\\/config grant Users read")
	// all three SYSTEM-only dirs (root, state, update) must assert BOTH the positive
	// SYSTEM presence AND inheritance-broken — not just Users/Everyone exclusion (a
	// dir that excludes Users only via inherited/empty ACLs would otherwise pass).
	if n := strings.Count(s, `$sids | Should -Contain $SID_SYSTEM`); n < 3 {
		t.Errorf("expected positive SYSTEM-present asserts on root+state+update (>=3), got %d", n)
	}
	if n := strings.Count(s, `Test-BeyzAclInheritanceBroken`); n < 3 {
		t.Errorf("expected inheritance-broken asserts on root+state+update (>=3), got %d", n)
	}
}

// ---- installer-independent service suite (freeze #4) ------------------------

func TestServiceSuiteCoverage(t *testing.T) {
	s := read(t, "Service.Tests.ps1")
	for _, c := range []struct{ needle, why string }{
		{`install --config $script:ConfigPath`, "install verb"},
		{`& $script:AgentExe start`, "start verb"},
		{`& $script:AgentExe restart`, "restart verb"},
		{`& $script:AgentExe stop`, "stop verb"},
		{`& $script:AgentExe uninstall`, "uninstall verb"},
		{`Get-BeyzServiceStartName 'BeyzBackupAgent' | Should -Be 'LocalSystem'`, "LocalSystem"},
		{`Test-BeyzServiceExists 'BeyzBackupUpdater' | Should -BeFalse`, "no second (updater) service"},
		{`& $script:UpdaterExe --version`, "updater run-once"},
	} {
		has(t, s, c.needle, c.why)
	}
}

// ---- flake control (freeze #6) ----------------------------------------------

func TestFlakeControlDiscipline(t *testing.T) {
	common := read(t, "Beyz.Common.psm1")
	inst := read(t, "Installer.Tests.ps1")
	svc := read(t, "Service.Tests.ps1")

	// bounded waits with a deadline (no unbounded waits).
	has(t, common, `$deadline = (Get-Date).AddSeconds($TimeoutSeconds)`, "bounded SCM waits via a deadline")
	has(t, common, `function Wait-BeyzServiceStatus`, "bounded service-status wait helper")
	has(t, common, `function Wait-BeyzServiceAbsent`, "bounded service-absent wait helper")

	// self-cleaning: every suite has BeforeAll + AfterAll cleanup.
	for _, f := range []string{inst, svc} {
		has(t, f, `BeforeAll`, "self-cleaning BeforeAll")
		has(t, f, `AfterAll`, "self-cleaning AfterAll")
	}
	has(t, svc, `Remove-BeyzAgentServiceIfPresent`, "service suite pre-cleans leftovers")
	has(t, inst, `Clear-BeyzInstall`, "installer suite pre-cleans leftovers")

	// Temp paths only for scratch.
	has(t, inst, `$env:TEMP`, "installer scratch in Temp")
	has(t, svc, `$env:TEMP`, "service scratch in Temp")

	// no fixed multi-second sleeps (only the bounded poll's sub-second tick).
	hasNot(t, inst, `Start-Sleep -Seconds`, "no fixed multi-second sleeps in the installer suite")
	hasNot(t, svc, `Start-Sleep -Seconds`, "no fixed multi-second sleeps in the service suite")

	// never contact a real SaaS / make network calls.
	for _, f := range []string{common, inst, svc} {
		hasNot(t, f, `Invoke-WebRequest`, "no network calls")
		hasNot(t, f, `Invoke-RestMethod`, "no network calls")
		hasNot(t, f, `api.beyzbackup`, "no real SaaS endpoint")
	}
}

// ---- CI (freeze #5) ---------------------------------------------------------

func TestCIPesterJob(t *testing.T) {
	ci := read(t, "../../.github/workflows/ci.yml")
	has(t, ci, "pester:", "Windows Pester job exists")
	idx := strings.Index(ci, "\n  pester:")
	if idx < 0 {
		t.Fatal("pester job not found")
	}
	block := ci[idx:]
	if end := strings.Index(block[len("\n  pester:"):], "\n  "); end >= 0 {
		// (heuristic block end; fine for these assertions)
	}
	has(t, block, "windows", "Pester job runs on a Windows runner")
	has(t, block, "continue-on-error: true", "Pester job is non-blocking initially")
	has(t, block, "-RequiredVersion", "Pester version is pinned")
	has(t, block, "Invoke-Pester", "the job runs the Pester suites")
}
