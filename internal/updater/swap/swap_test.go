package swap_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeebo/blake3"

	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// elfData returns n bytes with a valid ELF magic header.
func elfData(marker string, n int) []byte {
	b := make([]byte, n)
	b[0], b[1], b[2], b[3] = 0x7F, 'E', 'L', 'F'
	copy(b[4:], marker)
	for i := 4 + len(marker); i < n; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func artFor(data []byte) manifest.Artifact {
	s := sha256.Sum256(data)
	bl := blake3.Sum256(data)
	return manifest.Artifact{
		Platform: "linux", Arch: "amd64", URL: "https://dl.example.com/agent",
		SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(s[:]), BLAKE3: hex.EncodeToString(bl[:]),
	}
}

// fakeDownloader writes fixed bytes, enforcing the size cap like the real one.
type fakeDownloader struct {
	data []byte
	err  error
}

func (d *fakeDownloader) Download(_ context.Context, _ string, dst io.Writer, maxBytes int64) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	n, err := io.Copy(dst, io.LimitReader(bytes.NewReader(d.data), maxBytes+1))
	if err != nil {
		return n, err
	}
	if n > maxBytes {
		return n, swap.ErrSizeExceeded
	}
	return n, nil
}

type fixture struct {
	layout  swap.Layout
	oldData []byte
}

func setup(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "beyz-backup-agent")
	old := elfData("OLD", 96)
	if err := os.WriteFile(live, old, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("old-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture{layout: swap.Layout{LiveBinary: live, LiveConfig: cfg, StagingDir: staging}, oldData: old}
}

func newSwapper(t *testing.T, l swap.Layout, dl swap.Downloader) *swap.Swapper {
	t.Helper()
	s, err := swap.New(l, dl, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNewValidatesLayout(t *testing.T) {
	if _, err := swap.New(swap.Layout{StagingDir: "x"}, &fakeDownloader{}, 0); !errors.Is(err, swap.ErrLayout) {
		t.Errorf("missing LiveBinary: err = %v, want ErrLayout", err)
	}
	if _, err := swap.New(swap.Layout{LiveBinary: "x"}, &fakeDownloader{}, 0); !errors.Is(err, swap.ErrLayout) {
		t.Errorf("missing StagingDir: err = %v, want ErrLayout", err)
	}
}

func TestStageVerifiesAndStages(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 128)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	staged := filepath.Join(f.layout.StagingDir, "beyz-backup-agent.new")
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("staged .new not created: %v", err)
	}
	if !bytes.Equal(got, newData) {
		t.Error("staged bytes != downloaded bytes")
	}
	// download.tmp must be consumed (promoted)
	if _, err := os.Stat(filepath.Join(f.layout.StagingDir, "download.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Error("download.tmp should be promoted/removed")
	}
}

func TestStageHashMismatch(t *testing.T) {
	f := setup(t)
	good := elfData("NEW", 128)
	art := artFor(good)
	// download different bytes than the artifact's hashes describe
	s := newSwapper(t, f.layout, &fakeDownloader{data: elfData("EVIL", 128)})
	if err := s.Stage(context.Background(), art, "linux"); !errors.Is(err, verify.ErrHashMismatch) {
		t.Errorf("hash mismatch: err = %v, want verify.ErrHashMismatch", err)
	}
	if _, err := os.Stat(filepath.Join(f.layout.StagingDir, "beyz-backup-agent.new")); !errors.Is(err, os.ErrNotExist) {
		t.Error("no .new must be staged on hash mismatch")
	}
}

func TestStageSizeExceeded(t *testing.T) {
	f := setup(t)
	data := elfData("BIG", 4096)
	art := artFor(data)
	art.SizeBytes = 16 // signed cap far below the served bytes
	s := newSwapper(t, f.layout, &fakeDownloader{data: data})
	if err := s.Stage(context.Background(), art, "linux"); !errors.Is(err, swap.ErrSizeExceeded) {
		t.Errorf("oversized: err = %v, want ErrSizeExceeded", err)
	}
}

func TestStageNotExecutable(t *testing.T) {
	f := setup(t)
	// hash-valid bytes that are NOT a valid ELF (no magic) for a linux target
	notExe := []byte("this is definitely not an ELF binary, just text padding............")
	s := newSwapper(t, f.layout, &fakeDownloader{data: notExe})
	err := s.Stage(context.Background(), artFor(notExe), "linux")
	if !errors.Is(err, swap.ErrNotExecutable) || !errors.Is(err, swap.ErrStage) {
		t.Errorf("non-executable: err = %v, want ErrStage+ErrNotExecutable", err)
	}
	if _, err := os.Stat(filepath.Join(f.layout.StagingDir, "beyz-backup-agent.new")); !errors.Is(err, os.ErrNotExist) {
		t.Error("no .new must be staged for a non-executable artifact")
	}
}

func TestStageDownloadError(t *testing.T) {
	f := setup(t)
	s := newSwapper(t, f.layout, &fakeDownloader{err: swap.ErrDownload})
	if err := s.Stage(context.Background(), artFor(elfData("X", 64)), "linux"); !errors.Is(err, swap.ErrDownload) {
		t.Errorf("download error: err = %v, want ErrDownload", err)
	}
}

func TestSwapNotStaged(t *testing.T) {
	f := setup(t)
	s := newSwapper(t, f.layout, &fakeDownloader{})
	if err := s.Swap(); !errors.Is(err, swap.ErrNotStaged) {
		t.Errorf("swap without stage: err = %v, want ErrNotStaged", err)
	}
}

// Full cycle: stage -> backup -> swap -> restore round-trips old->new->old.
func TestFullCycleStageSwapRestore(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 160)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})

	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	dg, err := s.Backup()
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := s.Swap(); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	// live is now the new binary
	live, _ := os.ReadFile(f.layout.LiveBinary)
	if !bytes.Equal(live, newData) {
		t.Fatal("after swap, live != new binary")
	}
	// rollback restores the old binary (integrity-checked)
	if err := s.Restore(dg); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	live, _ = os.ReadFile(f.layout.LiveBinary)
	if !bytes.Equal(live, f.oldData) {
		t.Error("after restore, live != old binary")
	}
}

func TestRestoreRejectsCorruptBackup(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 160)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	dg, err := s.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Swap(); err != nil {
		t.Fatal(err)
	}
	// corrupt the .bak after the digest was recorded
	bak := filepath.Join(f.layout.StagingDir, "beyz-backup-agent.bak")
	if err := os.WriteFile(bak, []byte("corrupted backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(dg); !errors.Is(err, swap.ErrBackupCorrupt) {
		t.Errorf("corrupt backup: err = %v, want ErrBackupCorrupt", err)
	}
	// live must be unchanged (still the new binary), never a corrupt restore
	live, _ := os.ReadFile(f.layout.LiveBinary)
	if !bytes.Equal(live, newData) {
		t.Error("corrupt restore must not replace the live binary")
	}
}

func TestCommitCleansStaging(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 96)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	for _, name := range []string{"beyz-backup-agent.new", "beyz-backup-agent.bak", "config.yaml.bak", "download.tmp"} {
		if _, err := os.Stat(filepath.Join(f.layout.StagingDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should be removed by Commit", name)
		}
	}
}

func TestConfigBackedUpAndRestored(t *testing.T) {
	f := setup(t)
	newData := elfData("NEW", 96)
	s := newSwapper(t, f.layout, &fakeDownloader{data: newData})
	if err := s.Stage(context.Background(), artFor(newData), "linux"); err != nil {
		t.Fatal(err)
	}
	dg, _ := s.Backup()
	// config backup exists
	if _, err := os.Stat(filepath.Join(f.layout.StagingDir, "config.yaml.bak")); err != nil {
		t.Errorf("config backup missing: %v", err)
	}
	_ = s.Swap()
	// mutate the live config (as a "new" config), then roll back
	_ = os.WriteFile(f.layout.LiveConfig, []byte("new-config\n"), 0o644)
	if err := s.Restore(dg); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(f.layout.LiveConfig)
	if string(cfg) != "old-config\n" {
		t.Errorf("config not restored: %q", cfg)
	}
}

// ---- HTTPDownloader -----------------------------------------------------------

func TestHTTPDownloaderSuccess(t *testing.T) {
	body := elfData("DL", 200)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	d := swap.NewHTTPDownloader(srv.Client())
	var buf bytes.Buffer
	n, err := d.Download(context.Background(), srv.URL, &buf, 1<<20)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if n != int64(len(body)) || !bytes.Equal(buf.Bytes(), body) {
		t.Errorf("downloaded %d bytes, want %d", n, len(body))
	}
}

func TestHTTPDownloaderNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	d := swap.NewHTTPDownloader(srv.Client())
	if _, err := d.Download(context.Background(), srv.URL, io.Discard, 1<<20); !errors.Is(err, swap.ErrDownload) {
		t.Errorf("404: err = %v, want ErrDownload", err)
	}
}

func TestHTTPDownloaderSizeExceeded(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()
	d := swap.NewHTTPDownloader(srv.Client())
	if _, err := d.Download(context.Background(), srv.URL, io.Discard, 16); !errors.Is(err, swap.ErrSizeExceeded) {
		t.Errorf("oversized: err = %v, want ErrSizeExceeded", err)
	}
}

// A second successful Stage replaces the staged .new (the stale-clear happy path).
func TestStageReStageReplaces(t *testing.T) {
	f := setup(t)
	a := elfData("AAA", 96)
	s := newSwapper(t, f.layout, &fakeDownloader{data: a})
	if err := s.Stage(context.Background(), artFor(a), "linux"); err != nil {
		t.Fatal(err)
	}
	b := elfData("BBB", 128)
	s2, _ := swap.New(f.layout, &fakeDownloader{data: b}, 0)
	if err := s2.Stage(context.Background(), artFor(b), "linux"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(f.layout.StagingDir, "beyz-backup-agent.new"))
	if !bytes.Equal(got, b) {
		t.Error("re-stage must replace the staged binary with the new one")
	}
}
