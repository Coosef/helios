package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallerPowerShellScriptsParse parse-validates every installer PowerShell
// helper with the PowerShell engine. The installer invokes these at runtime
// (powershell.exe -File ...), so a syntax error makes the install fail closed —
// a class of bug the string-content tests below CANNOT catch (a present-but-
// malformed string still "contains" its substring). Skips when pwsh is absent;
// GitHub ubuntu/windows runners ship pwsh, so it runs in CI.
func TestInstallerPowerShellScriptsParse(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not on PATH; PowerShell parse-validation runs in CI (runners ship pwsh)")
	}
	scripts, err := filepath.Glob("scripts/*.ps1")
	if err != nil {
		t.Fatalf("glob installer/scripts/*.ps1: %v", err)
	}
	if len(scripts) == 0 {
		t.Fatal("no installer/scripts/*.ps1 found")
	}
	for _, s := range scripts {
		abs, _ := filepath.Abs(s)
		// Parse the file with the .NET PS parser; emit each parser error and exit
		// non-zero if any. (`exit 0` only on a clean parse.)
		ps := `$e=$null;` +
			`[void][System.Management.Automation.Language.Parser]::ParseFile('` + abs + `',[ref]$null,[ref]$e);` +
			`if($e -and $e.Count){$e|ForEach-Object{[Console]::Error.WriteLine("line $($_.Extent.StartLineNumber): $($_.Message)")};exit 1}`
		out, err := exec.Command(pwsh, "-NoProfile", "-Command", ps).CombinedOutput()
		if err != nil {
			t.Errorf("%s has PowerShell parse errors:\n%s", s, strings.TrimSpace(string(out)))
		}
	}
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

// ---- layout (freeze #3) -----------------------------------------------------

func TestProgramFilesLayout(t *testing.T) {
	has(t, InnoScript, `DefaultDirName={commonpf}\BeyzBackup`, "Program Files install root")
	has(t, InnoScript, `"beyz-backup-agent.exe"`, "agent binary installed")
	has(t, InnoScript, `"beyz-backup-updater.exe"`, "updater binary installed")
}

func TestProgramDataTree(t *testing.T) {
	for _, d := range []string{
		`Name: "{commonappdata}\BeyzBackup"`,
		`Name: "{commonappdata}\BeyzBackup\state"`,
		`Name: "{commonappdata}\BeyzBackup\logs"`,
		`Name: "{commonappdata}\BeyzBackup\update"`,
	} {
		has(t, InnoScript, d, "ProgramData tree dir")
	}
	has(t, InnoScript, `DestName: "config.yaml"`, "config.yaml laid into ProgramData")
}

// ---- ACL model (freeze #4) --------------------------------------------------

func TestACLMatrix(t *testing.T) {
	a := ApplyACLsScript
	has(t, a, `S-1-5-18`, "SYSTEM SID (locale-independent)")
	has(t, a, `S-1-5-32-544`, "Administrators SID")
	has(t, a, `S-1-5-32-545`, "Users SID")
	has(t, a, `/inheritance:r`, "inheritance broken on every node")
	has(t, a, `@($state, $update)`, "state + update locked to SYSTEM+Admins only")
	has(t, a, `${SID_USERS}:(OI)(CI)RX`, "logs: Users read+execute")
	has(t, a, `${SID_USERS}:R`, "config.yaml: Users read")
	has(t, a, `exit 1`, "fail-closed when icacls fails")
	has(t, a, `state ACL still grants Users`, "post-hardening Users/Everyone verification")
}

func TestEnrollTokenFileSystemOnlyACL(t *testing.T) {
	// The one-shot token file is locked to SYSTEM ONLY (Administrators excluded),
	// tighter than the rest of state\.
	has(t, InnoScript, `state\enroll-token`, "token file path matches the agent's compiled default")
	has(t, InnoScript, `/inheritance:r /grant:r *S-1-5-18:F`, "token file SYSTEM-only ACL")
	// the token-lock line must not grant Administrators/Users.
	for _, ln := range strings.Split(InnoScript, "\n") {
		if strings.Contains(ln, `tokenPath`) && strings.Contains(ln, `icacls`) {
			hasNot(t, ln, `S-1-5-32-544`, "token ACL must not grant Administrators")
			hasNot(t, ln, `S-1-5-32-545`, "token ACL must not grant Users")
		}
	}
	// FAIL-CLOSED: the lock must be exit-code-checked, and on failure the token is
	// DELETED + the install aborted (a bearer token must never linger under the
	// weaker inherited ACL). A static syntax-only check would be false assurance.
	has(t, InnoScript, `DeleteFile(tokenPath)`, "token removed if its SYSTEM-only ACL cannot be applied")
	has(t, InnoScript, `resExit)) or (resExit <> 0) then`, "token-lock checks the icacls exit code and aborts")
}

// ---- token handling (freeze #5) ---------------------------------------------

func TestTokenHandling(t *testing.T) {
	s := InnoScript
	has(t, s, `{param:TOKENFILE|}`, "/TOKENFILE= (log-safe) silent path")
	has(t, s, `{param:TOKEN|}`, "/TOKEN= silent path supported")
	has(t, s, `TokenPage.Add('Enrollment token:', True)`, "masked interactive wizard input")
	has(t, s, `state\enroll-token`, "token written to the agent's one-shot file")
	has(t, s, `Trim(raw)`, "token trimmed of surrounding whitespace")
}

func TestTokenNeverLoggedOrInConfig(t *testing.T) {
	s := InnoScript
	// The token VALUE is written only via SaveStringToFile (a file API), never as
	// an Exec/[Run] parameter, so it cannot reach the Inno command-line log.
	has(t, s, `SaveStringToFile(tokenPath, tok, False)`, "token written via file API, not a logged command line")
	// No [Run]/Exec line passes the token value. The icacls token-lock passes the
	// PATH only; the service [Run] entries never reference the token.
	hasNot(t, s, `Parameters: "install --config ""{commonappdata}\BeyzBackup\config.yaml""" --token`, "no token on the install command line")
	// config.sample.yaml (the laydown source) carries NO token KEY. (Its comments
	// legitimately mention the token to explain it is REJECTED there, so we check
	// only non-comment lines.)
	sample, err := os.ReadFile("../configs/config.sample.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range strings.Split(string(sample), "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "enrollment_token") {
			t.Errorf("config sample defines a token key in a non-comment line: %q", ln)
		}
	}
}

// ---- service flow (freeze #6) -----------------------------------------------

func TestServiceFlow(t *testing.T) {
	s := InnoScript
	has(t, s, `"BeyzBackupAgent"`, "service name BeyzBackupAgent")
	has(t, s, `install --config`, "register service via the agent's own install verb")
	has(t, s, `Parameters: "start"`, "start the service")
	has(t, s, `{sys}\sc.exe`, "sc.exe for start-type + recovery")
	has(t, s, `start= auto`, "automatic start")
	has(t, s, `failure {#ServiceName}`, "failure recovery actions")
}

func TestUpdaterIsNotAService(t *testing.T) {
	s := InnoScript
	has(t, s, `{#UpdaterExe}`, "updater binary is installed")
	// ...but never registered as a service.
	hasNot(t, s, `{#UpdaterExe}"; Parameters: "install`, "updater must not be service-installed")
	hasNot(t, s, `create BeyzBackupUpdater`, "no second (updater) service is ever created")
	hasNot(t, s, `updater.exe"; Parameters: "install`, "no updater install verb invocation")
}

// ---- upgrade + uninstall (freeze #8/#9) -------------------------------------

func TestUpgradeStopsServiceAndAntiDowngrade(t *testing.T) {
	s := InnoScript
	has(t, s, `function PrepareToInstall`, "stop service before replacing binaries")
	has(t, s, `stop {#ServiceName}`, "service stopped on upgrade")
	has(t, s, `function InitializeSetup`, "anti-downgrade guard")
	has(t, s, `Downgrade is not supported`, "refuse install over a newer version")
}

func TestUninstallAndKeepState(t *testing.T) {
	s := InnoScript
	has(t, s, `Parameters: "stop"`, "uninstall stops the service")
	has(t, s, `Parameters: "uninstall"`, "uninstall removes the service")
	has(t, s, `{param:KEEPSTATE|0}`, "/KEEPSTATE flag")
	has(t, s, `DelTree(root, True, True, True)`, "default: shred state by removing the tree")
	has(t, s, `DelTree(root + '\logs'`, "KEEPSTATE preserves state\\, removes logs")
	has(t, s, `BeyzBackupUpdater`, "defensively remove any updater scheduled task")
}

// ---- signing-ready but unsigned (freeze #2) ---------------------------------

func TestUnsignedButSigningReady(t *testing.T) {
	// No ACTIVE SignTool directive (a real one would be at line start, no ';').
	hasNot(t, InnoScript, "\nSignTool=", "installer must ship UNSIGNED (Authenticode = T30)")
	has(t, InnoScript, "; SignTool=signtool", "signing-ready hook is present + documented")
}

// ---- CI (freeze #11) --------------------------------------------------------

func TestCIInnoBuildJobNonBlocking(t *testing.T) {
	b, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	has(t, ci, "installer-build:", "non-blocking Windows Inno build job exists")
	// the job block (from its header to the next top-level job) must be windows +
	// non-blocking + actually invoke ISCC.
	idx := strings.Index(ci, "\n  installer-build:")
	if idx < 0 {
		t.Fatal("installer-build job not found")
	}
	rest := ci[idx+1:]
	if next := strings.Index(rest[len("  installer-build:"):], "\n  "); next >= 0 {
		// trim to the next 2-space-indented job header (heuristic; fine for assertions)
	}
	block := rest
	if end := strings.Index(rest, "\n\n  "); end > 0 {
		block = rest[:end]
	}
	has(t, block, "windows", "Inno build runs on a Windows runner")
	has(t, block, "continue-on-error: true", "Inno build job is non-blocking")
	has(t, block, "ISCC", "the job invokes the Inno compiler")
}
