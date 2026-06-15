// Package dist holds S1-T30 static checks for the version-info + Authenticode
// signing pipeline. They run everywhere (incl. `go test ./...`) and pin: the
// single version source is unified across ldflags / Windows version-info / Inno
// AppVersion / artifact names; the signing helper is secret-safe + fail-closed;
// no .syso / cert / private key is ever committed; and PR CI never requires
// signing secrets.
package dist

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
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

func canonicalVersion(t *testing.T) string {
	t.Helper()
	v := strings.TrimSpace(read(t, "VERSION"))
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		t.Fatalf("VERSION %q is not numeric x.y.z", v)
	}
	return v
}

// ---- version unification (freeze #4) ----------------------------------------

type versionInfo struct {
	FixedFileInfo struct {
		FileVersion    struct{ Major, Minor, Patch, Build int }
		ProductVersion struct{ Major, Minor, Patch, Build int }
	}
	StringFileInfo struct {
		FileVersion    string
		ProductVersion string
		CompanyName    string
		ProductName    string
	}
}

func TestVersionUnification(t *testing.T) {
	v := canonicalVersion(t)
	parts := strings.Split(v, ".")

	// .iss AppVersion fallback must equal VERSION (no drift; /DAppVersion overrides).
	iss := read(t, "installer/beyz-backup.iss")
	has(t, iss, `#define AppVersion "`+v+`"`, "Inno AppVersion fallback == VERSION")
	has(t, iss, `OutputBaseFilename=BeyzBackupSetup-{#AppVersion}`, "installer artifact tracks the version")

	// Both version-info JSONs must encode the same VERSION.
	for _, f := range []string{"build/windows/versioninfo.agent.json", "build/windows/versioninfo.updater.json"} {
		var vi versionInfo
		if err := json.Unmarshal([]byte(read(t, f)), &vi); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got := []int{vi.FixedFileInfo.FileVersion.Major, vi.FixedFileInfo.FileVersion.Minor, vi.FixedFileInfo.FileVersion.Patch}
		for i, p := range parts {
			if want := atoi(p); got[i] != want {
				t.Errorf("%s FixedFileInfo.FileVersion[%d] = %d, want %d (VERSION drift)", f, i, got[i], want)
			}
		}
		if vi.StringFileInfo.FileVersion != v+".0" {
			t.Errorf("%s StringFileInfo.FileVersion = %q, want %q", f, vi.StringFileInfo.FileVersion, v+".0")
		}
		if vi.StringFileInfo.ProductVersion != v+".0" {
			t.Errorf("%s StringFileInfo.ProductVersion = %q, want %q", f, vi.StringFileInfo.ProductVersion, v+".0")
		}
	}

	// Taskfile sources VERSION from the file and injects ldflags incl. Channel.
	tf := read(t, "Taskfile.yml")
	has(t, tf, `< VERSION`, "Taskfile reads the VERSION file")
	has(t, tf, `.Channel={{.CHANNEL}}`, "ldflags inject the channel")
	has(t, tf, `/DAppVersion={{.VERSION}}`, "installer build passes the unified version to ISCC")
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// ---- Taskfile dist pipeline (freeze #1/#10) ---------------------------------

func TestTaskfileDistTargets(t *testing.T) {
	tf := read(t, "Taskfile.yml")
	for _, target := range []string{"gen:versioninfo:", "cross:", "dist:", "installer:"} {
		has(t, tf, target, "Taskfile target "+target)
	}
	has(t, tf, "goversioninfo@v1.4.1", "goversioninfo is pinned")
	has(t, tf, "SHA256SUMS", "dist produces SHA256SUMS")
	// artifact names
	has(t, tf, "beyz-backup-agent.exe", "windows agent artifact name")
	has(t, tf, "beyz-backup-updater.exe", "windows updater artifact name")
}

// ---- sign script secret-safety + fail-closed (freeze #6/#8) -----------------

func TestSignScriptSecretSafeAndFailClosed(t *testing.T) {
	s := read(t, "installer/scripts/sign.ps1")
	// cert sourced ONLY from env, never a literal.
	has(t, s, "BEYZ_SIGN_THUMBPRINT", "thumbprint from env")
	has(t, s, "BEYZ_SIGN_PFX_BASE64", "pfx from env")
	has(t, s, "BEYZ_SIGN_PFX_PASSWORD", "pfx password from env")
	// SHA-256 digest + RFC3161 timestamp.
	has(t, s, "/fd", "sha256 file digest")
	has(t, s, "sha256", "sha256")
	has(t, s, "/tr", "RFC3161 timestamp")
	has(t, s, "/td", "timestamp digest")
	// fail-closed: skip when unconfigured, exit non-zero on sign/verify failure or
	// RequireSigned-without-cert.
	has(t, s, "RequireSigned", "release jobs can require signing")
	has(t, s, "verify /pa", "verify after sign")
	has(t, s, "exit 1", "fail closed on sign/verify failure")
	has(t, s, "exit 2", "fail closed when a required cert is missing")
	// temp pfx deleted.
	has(t, s, "finally", "temp pfx cleaned in finally")
	has(t, s, "Remove-Item -LiteralPath $pfxPath", "temp pfx removed")
	// NEVER log the password: no Write-Host/echo of the password var, and the
	// signtool arg array (which carries /p <password>) is not printed.
	hasNot(t, s, "Write-Host $env:BEYZ_SIGN_PFX_PASSWORD", "password never logged")
	hasNot(t, s, "Write-Output $env:BEYZ_SIGN_PFX_PASSWORD", "password never logged")
	hasNot(t, s, "Write-Host $signArgs", "signtool args (with password) never logged")
}

// ---- self-signed smoke (freeze #7) ------------------------------------------

func TestSelfSignedSmokeScript(t *testing.T) {
	s := read(t, "build/windows/sign-smoke.ps1")
	has(t, s, "New-SelfSignedCertificate", "ephemeral self-signed cert (no real secret)")
	has(t, s, "Cert:\\CurrentUser\\Root", "trust the test cert so verify /pa passes")
	has(t, s, "TrustedPublisher", "trust the test cert as a publisher")
	has(t, s, "sign.ps1", "exercises the real sign.ps1 code path")
	has(t, s, "-RequireSigned", "a missing cert would fail the smoke")
	has(t, s, "finally", "test cert removed from the stores afterwards")
}

// ---- nothing dangerous committed (freeze #5/#8) -----------------------------

func TestNoCommittedSysoOrSecrets(t *testing.T) {
	// Check TRACKED files only (a generated .syso may legitimately exist on disk,
	// gitignored — what must never happen is committing one, or a cert/key).
	out, err := exec.Command("git", "-C", filepath.Join("..", ".."), "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var offenders []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".syso", ".pfx", ".p12", ".key", ".crt", ".cer":
			offenders = append(offenders, f)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("committed .syso / cert / private-key files (must never be tracked): %v", offenders)
	}
}

// ---- CI safety: no signing secrets on PR; self-signed smoke present (freeze #7) ----

func TestCISigningIsSecretFreeAndSmokePresent(t *testing.T) {
	ci := read(t, ".github/workflows/ci.yml")
	has(t, ci, "sign-smoke:", "self-signed signing smoke job exists")
	// the entire CI workflow must not reference any signing secret (PR-safe).
	hasNot(t, ci, "secrets.", "no CI secrets referenced anywhere in PR-triggered CI")
	hasNot(t, ci, "BEYZ_SIGN_PFX_PASSWORD", "no real signing secret wired into PR CI")
	// the sign-smoke job is non-blocking + runs the self-signed smoke.
	idx := strings.Index(ci, "\n  sign-smoke:")
	if idx < 0 {
		t.Fatal("sign-smoke job not found")
	}
	block := ci[idx:]
	has(t, block, "windows", "sign-smoke runs on a Windows runner")
	has(t, block, "continue-on-error: true", "sign-smoke is non-blocking initially")
	has(t, block, "sign-smoke.ps1", "sign-smoke runs the self-signed smoke")
}

func ciJobBlock(ci, header string) string {
	idx := strings.Index(ci, "\n  "+header)
	if idx < 0 {
		return ""
	}
	rest := ci[idx+1:]
	if end := strings.Index(rest, "\n\n  "); end > 0 {
		return rest[:end]
	}
	return rest
}

// Version unification must hold across ALL CI distributable builds, not just the
// Taskfile (the gap the adversarial review caught): every ISCC invocation passes
// /DAppVersion, and the uploaded installer-build artifact is version-stamped.
func TestCIInstallerJobsUnifyVersion(t *testing.T) {
	ci := read(t, ".github/workflows/ci.yml")
	iscc := 0
	for _, ln := range strings.Split(ci, "\n") {
		if strings.Contains(ln, "ISCC.exe") {
			iscc++
			if !strings.Contains(ln, "/DAppVersion=") {
				t.Errorf("ISCC invocation without /DAppVersion (version can drift from VERSION): %s", strings.TrimSpace(ln))
			}
		}
	}
	if iscc == 0 {
		t.Fatal("no ISCC invocation found in ci.yml")
	}
	ib := ciJobBlock(ci, "installer-build:")
	has(t, ib, "goversioninfo@v1.4.1", "installer-build embeds version-info into the distributable binaries")
	has(t, ib, "buildinfo.Version=$v", "installer-build version-stamps the binaries (not 0.0.0-dev)")
	has(t, ib, "/DAppVersion=$v", "installer-build unifies the installer version")
}
