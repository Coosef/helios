package swap_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/pkg/hashing"
)

func TestNewWithDefaultDownloader(t *testing.T) {
	dir := t.TempDir()
	s, err := swap.New(swap.Layout{LiveBinary: filepath.Join(dir, "a"), StagingDir: dir}, nil, 0)
	if err != nil || s == nil {
		t.Fatalf("New with default downloader: %v", err)
	}
}

func TestHTTPDownloaderDefaultClientError(t *testing.T) {
	d := swap.NewHTTPDownloader(nil) // hardened default client
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled -> Do fails immediately
	if _, err := d.Download(ctx, "https://example.com/agent", io.Discard, 1<<20); !errors.Is(err, swap.ErrDownload) {
		t.Errorf("cancelled download: err = %v, want ErrDownload", err)
	}
}

func TestHTTPDownloaderBadURL(t *testing.T) {
	d := swap.NewHTTPDownloader(nil)
	if _, err := d.Download(context.Background(), "://not a url", io.Discard, 1<<20); !errors.Is(err, swap.ErrDownload) {
		t.Errorf("bad url: err = %v, want ErrDownload", err)
	}
}

func TestStageDownloadOpenFailure(t *testing.T) {
	// StagingDir points at a non-existent path -> opening download.tmp fails.
	dir := t.TempDir()
	l := swap.Layout{LiveBinary: filepath.Join(dir, "agent"), StagingDir: filepath.Join(dir, "missing", "staging")}
	s, err := swap.New(l, &fakeDownloader{data: elfData("X", 64)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Stage(context.Background(), artFor(elfData("X", 64)), "linux"); !errors.Is(err, swap.ErrDownload) {
		t.Errorf("staging into a missing dir: err = %v, want ErrDownload", err)
	}
}

func TestBackupMissingLiveBinary(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	_ = os.MkdirAll(staging, 0o700)
	l := swap.Layout{LiveBinary: filepath.Join(dir, "does-not-exist"), StagingDir: staging}
	s, _ := swap.New(l, &fakeDownloader{}, 0)
	if _, err := s.Backup(); !errors.Is(err, swap.ErrBackup) {
		t.Errorf("backup of missing binary: err = %v, want ErrBackup", err)
	}
}

func TestRestoreMissingBackup(t *testing.T) {
	f := setup(t)
	s := newSwapper(t, f.layout, &fakeDownloader{})
	dg, _ := hashing.HashBytes(hashing.BLAKE3, []byte("nonexistent"))
	if err := s.Restore(dg); !errors.Is(err, swap.ErrRestore) {
		t.Errorf("restore without backup: err = %v, want ErrRestore", err)
	}
}

func TestNoConfigLayoutBackupRestore(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	_ = os.MkdirAll(staging, 0o700)
	live := filepath.Join(dir, "agent")
	_ = os.WriteFile(live, elfData("OLD", 64), 0o755)
	l := swap.Layout{LiveBinary: live, StagingDir: staging} // no LiveConfig
	newData := elfData("NEW", 80)
	s, _ := swap.New(l, &fakeDownloader{data: newData}, 0)
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	dg, err := s.Backup()
	if err != nil {
		t.Fatalf("Backup (no config): %v", err)
	}
	// no config.bak should exist
	matches, _ := filepath.Glob(filepath.Join(staging, "*.yaml.bak"))
	if len(matches) != 0 {
		t.Errorf("unexpected config backup: %v", matches)
	}
	if err := s.Swap(); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(dg); err != nil {
		t.Errorf("Restore (no config): %v", err)
	}
}

func TestBackupStagingReadOnly(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	_ = os.MkdirAll(staging, 0o700)
	live := filepath.Join(dir, "agent")
	_ = os.WriteFile(live, elfData("OLD", 64), 0o755)
	// make the staging dir non-writable so the .bak copy's open(dst) fails
	if err := os.Chmod(staging, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(staging, 0o700) })
	s, _ := swap.New(swap.Layout{LiveBinary: live, StagingDir: staging}, &fakeDownloader{}, 0)
	if _, err := s.Backup(); !errors.Is(err, swap.ErrBackup) {
		t.Errorf("backup into a read-only staging dir: err = %v, want ErrBackup", err)
	}
}

func TestRestoreConfigWriteFailure(t *testing.T) {
	// Config lives in its own dir so we can make it read-only independently of the
	// binary. The atomic config restore copies to a temp in that dir, then renames;
	// a read-only config dir makes the temp creation fail -> ErrRestore.
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	_ = os.MkdirAll(staging, 0o700)
	live := filepath.Join(dir, "agent")
	_ = os.WriteFile(live, elfData("OLD", 64), 0o755)
	confDir := filepath.Join(dir, "conf")
	_ = os.MkdirAll(confDir, 0o700)
	cfg := filepath.Join(confDir, "config.yaml")
	_ = os.WriteFile(cfg, []byte("old\n"), 0o644)

	newData := elfData("NEW", 96)
	s, _ := swap.New(swap.Layout{LiveBinary: live, LiveConfig: cfg, StagingDir: staging}, &fakeDownloader{data: newData}, 0)
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	dg, _ := s.Backup()
	_ = s.Swap()
	if err := os.Chmod(confDir, 0o500); err != nil { // read-only config dir
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(confDir, 0o700) })
	if err := s.Restore(dg); !errors.Is(err, swap.ErrRestore) {
		t.Errorf("restore with read-only config dir: err = %v, want ErrRestore", err)
	}
	// the binary was still restored atomically (live == old) before the config step
	got, _ := os.ReadFile(live)
	if string(got[:3]) != string(elfData("OLD", 64)[:3]) {
		t.Error("binary should be atomically restored even if config restore fails")
	}
}

// A failed re-Stage must NOT leave a consumable .new from a prior successful Stage
// (otherwise Swap could install the wrong binary).
func TestStageClearsStaleNewOnFailure(t *testing.T) {
	f := setup(t)
	dataA := elfData("AAA", 96)
	s := newSwapper(t, f.layout, &fakeDownloader{data: dataA})
	if err := s.Stage(context.Background(), artFor(dataA), "linux"); err != nil {
		t.Fatal(err) // A staged
	}
	// Re-Stage B but with a hash mismatch (downloads other bytes) -> Stage fails.
	dataB := elfData("BBB", 96)
	s2, _ := swap.New(f.layout, &fakeDownloader{data: elfData("EVIL", 96)}, 0)
	if err := s2.Stage(context.Background(), artFor(dataB), "linux"); err == nil {
		t.Fatal("re-Stage should have failed (hash mismatch)")
	}
	// The stale A .new must be gone -> Swap finds nothing to swap.
	if err := s2.Swap(); !errors.Is(err, swap.ErrNotStaged) {
		t.Errorf("after a failed re-Stage, Swap = %v, want ErrNotStaged (no stale .new)", err)
	}
}

func TestCommitReportsRemovalFailure(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 64)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(); err != nil {
		t.Fatal(err)
	}
	// read-only staging dir -> os.Remove of the staged files fails -> Commit reports it
	if err := os.Chmod(f.layout.StagingDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.layout.StagingDir, 0o700) })
	if err := s.Commit(); err == nil {
		t.Error("Commit should report removal failures from a read-only staging dir")
	}
}

func TestBackupConfigSourceUnreadable(t *testing.T) {
	f := setup(t)
	s := newSwapper(t, f.layout, &fakeDownloader{})
	// binary readable (backup ok), config unreadable -> config snapshot fails.
	if err := os.Chmod(f.layout.LiveConfig, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.layout.LiveConfig, 0o644) })
	if _, err := s.Backup(); !errors.Is(err, swap.ErrBackup) {
		t.Errorf("unreadable config: err = %v, want ErrBackup", err)
	}
}
