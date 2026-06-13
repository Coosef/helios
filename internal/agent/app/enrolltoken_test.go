package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/agent/enroll"
)

// ---- test helpers -----------------------------------------------------------

// appDir builds an App over a fake builder with a real state directory (nil log
// -> the log helpers are no-ops, so the lifecycle is exercised without I/O noise).
func appDir(b builder, dir string) *App { return &App{b: b, stateDir: dir} }

func writeToken(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, enrollTokenFile), []byte(content), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
}

func tokenExists(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, enrollTokenFile))
	switch {
	case err == nil:
		return true
	case errors.Is(err, os.ErrNotExist):
		return false
	default:
		t.Fatalf("stat token file: %v", err)
		return false
	}
}

// noopLoops makes Heartbeat/Tasks return immediately so Run completes once it
// reaches the runtime phase (used by the success / already-enrolled cases).
func noopLoops(f *fakeBuilder) {
	f.hbRun = func(context.Context, chan<- struct{}) error { return nil }
	f.tkRun = func(context.Context, <-chan struct{}) error { return nil }
}

// ---- pure file helpers ------------------------------------------------------

func TestReadEnrollTokenFile_Absent(t *testing.T) {
	tok, res, err := readEnrollTokenFile(t.TempDir())
	if err != nil || res != tokenFileAbsent || tok != "" {
		t.Fatalf("absent: got (%q,%v,%v), want (\"\",tokenFileAbsent,nil)", tok, res, err)
	}
}

func TestReadEnrollTokenFile_Valid(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "  enroll-tok-123\n")
	tok, res, err := readEnrollTokenFile(dir)
	if err != nil || res != tokenFileValid {
		t.Fatalf("valid: got res=%v err=%v", res, err)
	}
	if tok != "enroll-tok-123" { // surrounding whitespace trimmed
		t.Fatalf("valid: token = %q, want trimmed %q", tok, "enroll-tok-123")
	}
}

func TestReadEnrollTokenFile_EmptyAndWhitespace(t *testing.T) {
	for _, content := range []string{"", "   ", "\n\t  \r\n"} {
		dir := t.TempDir()
		writeToken(t, dir, content)
		tok, res, err := readEnrollTokenFile(dir)
		if err != nil || res != tokenFileEmpty || tok != "" {
			t.Fatalf("empty(%q): got (%q,%v,%v), want (\"\",tokenFileEmpty,nil)", content, tok, res, err)
		}
	}
}

func TestReadEnrollTokenFile_Unreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory at the token path makes os.ReadFile fail with a non-not-exist
	// error on every OS (portable, unlike chmod which root bypasses).
	if err := os.Mkdir(filepath.Join(dir, enrollTokenFile), 0o700); err != nil {
		t.Fatal(err)
	}
	tok, res, err := readEnrollTokenFile(dir)
	if res != tokenFileUnreadable || err == nil || tok != "" {
		t.Fatalf("unreadable: got (%q,%v,err=%v), want (\"\",tokenFileUnreadable,non-nil)", tok, res, err)
	}
}

func TestPurgeEnrollTokenFile(t *testing.T) {
	dir := t.TempDir()
	// Idempotent on a missing file.
	if err := purgeEnrollTokenFile(dir); err != nil {
		t.Fatalf("purge(absent) = %v, want nil", err)
	}
	// Removes a present file.
	writeToken(t, dir, "x")
	if err := purgeEnrollTokenFile(dir); err != nil {
		t.Fatalf("purge(present) = %v, want nil", err)
	}
	if tokenExists(t, dir) {
		t.Fatal("purge(present): file still on disk")
	}
}

// ---- Run-level lifecycle ----------------------------------------------------

// file present, no env -> enrollment uses the file token.
func TestRun_FileToken_NoEnv_UsesFileToken(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: ""} // env empty
	noopLoops(f)
	if err := appDir(f, dir).Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(f.setTokens) != 1 || f.setTokens[0] != "FILETOK" {
		t.Fatalf("SetEnrollToken calls = %v, want [FILETOK]", f.setTokens)
	}
	if f.tokenAtEnrol != "FILETOK" {
		t.Fatalf("token at enroll = %q, want FILETOK", f.tokenAtEnrol)
	}
	if tokenExists(t, dir) {
		t.Fatal("token file not deleted after successful enroll")
	}
}

// env + file -> env wins (file is never read as a source).
func TestRun_EnvWins_FileNotReadAsSource(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: "ENVTOK"}
	noopLoops(f)
	if err := appDir(f, dir).Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(f.setTokens) != 0 {
		t.Fatalf("SetEnrollToken called %v, want none (env wins)", f.setTokens)
	}
	if f.tokenAtEnrol != "ENVTOK" {
		t.Fatalf("token at enroll = %q, want ENVTOK", f.tokenAtEnrol)
	}
}

// env used + file present -> the leftover file is still deleted after success.
func TestRun_EnvUsed_FilePresent_DeletedAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: "ENVTOK"}
	noopLoops(f)
	if err := appDir(f, dir).Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if tokenExists(t, dir) {
		t.Fatal("leftover token file not cleaned after env-sourced success")
	}
}

// neither env nor file -> no token resolved; the enroll error surfaces (terminal).
func TestRun_NoToken_TerminalNoEnrollmentToken(t *testing.T) {
	dir := t.TempDir()
	f := &fakeBuilder{token: "", enrollErr: enroll.ErrNoEnrollmentToken}
	noopLoops(f)
	err := appDir(f, dir).Run(context.Background())
	if !errors.Is(err, ErrEnrollFailed) {
		t.Fatalf("Run = %v, want wrapping ErrEnrollFailed", err)
	}
	if len(f.setTokens) != 0 || f.tokenAtEnrol != "" {
		t.Fatalf("token resolved unexpectedly: set=%v atEnrol=%q", f.setTokens, f.tokenAtEnrol)
	}
	if f.enrollCalls != 1 {
		t.Fatalf("enrollCalls = %d, want 1", f.enrollCalls)
	}
}

// empty file -> poison-pill deleted, treated as no token.
func TestRun_EmptyFile_PoisonPillDeleted(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "   \n")
	f := &fakeBuilder{token: "", enrollErr: enroll.ErrNoEnrollmentToken}
	noopLoops(f)
	_ = appDir(f, dir).Run(context.Background())
	if len(f.setTokens) != 0 || f.tokenAtEnrol != "" {
		t.Fatalf("empty file produced a token: set=%v atEnrol=%q", f.setTokens, f.tokenAtEnrol)
	}
	if tokenExists(t, dir) {
		t.Fatal("empty poison-pill file not deleted")
	}
}

// unreadable file -> preserved, treated as no token (fail closed).
func TestRun_UnreadableFile_PreservedNoToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, enrollTokenFile)
	if err := os.Mkdir(path, 0o700); err != nil { // directory => unreadable
		t.Fatal(err)
	}
	f := &fakeBuilder{token: "", enrollErr: enroll.ErrNoEnrollmentToken}
	noopLoops(f)
	_ = appDir(f, dir).Run(context.Background())
	if len(f.setTokens) != 0 || f.tokenAtEnrol != "" {
		t.Fatalf("unreadable file produced a token: set=%v atEnrol=%q", f.setTokens, f.tokenAtEnrol)
	}
	if _, err := os.Stat(path); err != nil { // NOT deleted
		t.Fatalf("unreadable token path was removed: %v", err)
	}
}

// success -> file deleted.
func TestRun_Success_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: ""} // enrollErr nil => success
	noopLoops(f)
	if err := appDir(f, dir).Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if tokenExists(t, dir) {
		t.Fatal("token file not deleted after success")
	}
}

// token rejected (401/409) -> file deleted + terminal re-enrollment-required.
func TestRun_TokenRejected_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: "", enrollErr: enroll.ErrTokenRejected}
	noopLoops(f)
	err := appDir(f, dir).Run(context.Background())
	if !errors.Is(err, ErrEnrollmentRequired) {
		t.Fatalf("Run = %v, want wrapping ErrEnrollmentRequired", err)
	}
	if tokenExists(t, dir) {
		t.Fatal("rejected token file not deleted")
	}
}

// transient failure -> file preserved (a retry can reuse the still-valid token).
func TestRun_TransientFailure_PreservesFile(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: "", enrollErr: errors.New("network blip")}
	noopLoops(f)
	err := appDir(f, dir).Run(context.Background())
	if err == nil {
		t.Fatal("Run = nil, want a transient enroll error")
	}
	if !tokenExists(t, dir) {
		t.Fatal("token file deleted on a transient failure (should be preserved)")
	}
}

// env token + file present + transient failure -> file PRESERVED. The cleanup is
// source-independent: only a DEFINITIVE outcome (success / rejected) deletes, so an
// env-sourced transient failure must keep the leftover file for a later retry. This
// locks in the "regardless of which source supplied the token" claim at app.go:197.
func TestRun_EnvUsed_FilePresent_TransientFailure_PreservesFile(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "FILETOK")
	f := &fakeBuilder{token: "ENVTOK", enrollErr: errors.New("network blip")}
	noopLoops(f)
	err := appDir(f, dir).Run(context.Background())
	if err == nil {
		t.Fatal("Run = nil, want a transient enroll error")
	}
	if len(f.setTokens) != 0 {
		t.Fatalf("SetEnrollToken called %v, want none (env wins)", f.setTokens)
	}
	if f.tokenAtEnrol != "ENVTOK" {
		t.Fatalf("token at enroll = %q, want ENVTOK", f.tokenAtEnrol)
	}
	if !tokenExists(t, dir) {
		t.Fatal("env+file transient failure deleted the file (should be preserved)")
	}
}

// already enrolled + lingering file -> file purged, no enroll attempted.
func TestRun_AlreadyEnrolled_PurgesFileNoEnroll(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "STALETOK")
	f := &fakeBuilder{enrolled: true}
	noopLoops(f)
	if err := appDir(f, dir).Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if f.enrollCalls != 0 {
		t.Fatalf("enrollCalls = %d, want 0 (no re-enroll)", f.enrollCalls)
	}
	if tokenExists(t, dir) {
		t.Fatal("lingering token file not purged on the already-enrolled path")
	}
}
