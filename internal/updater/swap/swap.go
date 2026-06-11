// Package swap performs the staged, atomic replacement of the AGENT binary (S1-T25,
// ADR-002 #8): download → dual-hash verify (T23) + executable-format sanity →
// stage → backup → atomic MoveFileEx/rename → integrity-checked rollback. It is a
// pure FILE-OPS layer.
//
// SCOPE (frozen): swaps the agent binary ONLY (no updater self-update — Sprint 8).
// It does NOT stop/start the service (T27 via the T18 service-control layer),
// persist updater_state.json or current_version (T27), drive the FSM (T27), emit
// audit (T27), or decide the health gate / when to roll back (T26). The agent MUST
// be stopped by the caller before Swap/Restore. Every failure aborts BEFORE the
// live binary is replaced, or rolls back; the live binary is never left truncated.
//
// Backup scope (binary, config) — NOT identity/enrollment state. Identity
// (device_id, certificate, session token) is server-authoritative, monotonic, and
// the session token rotates every heartbeat; restoring a stale snapshot would
// desync from the server (401 → re-enroll → brick) — strictly worse than keeping
// the current credentials. The state store is the agent's exclusively-locked bbolt
// and must not be written by the updater. The "old binary vs newer state schema"
// concern is an agent-side invariant (state forward-compat / migration deferred
// until commit), tracked as FI-T25-1 — not solved by rolling back identity.
package swap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/hashing"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// DefaultMaxArtifactBytes is the hard secondary ceiling on an artifact download (a
// backstop above the signed primary cap, manifest size_bytes). A Go agent binary
// is tens of MiB; 512 MiB is a generous DoS backstop.
const DefaultMaxArtifactBytes int64 = 512 << 20

var (
	// ErrLayout is returned for an invalid Layout (missing paths).
	ErrLayout = errors.New("swap: invalid layout")
	// ErrCrossVolume is returned when the staging dir is not on the same volume as
	// the live binary (atomic rename impossible).
	ErrCrossVolume = errors.New("swap: staging dir is not on the live binary's volume")
	// ErrNotStaged is returned by Swap when no verified .new file is staged.
	ErrNotStaged = errors.New("swap: no staged binary (call Stage first)")
	// ErrStage wraps a staging (download/verify/promote) failure.
	ErrStage = errors.New("swap: staging failed")
	// ErrBackup wraps a backup-snapshot failure.
	ErrBackup = errors.New("swap: backup failed")
	// ErrSwap wraps an atomic-replace failure.
	ErrSwap = errors.New("swap: atomic replace failed")
	// ErrBackupCorrupt is returned when the .bak integrity check fails before restore.
	ErrBackupCorrupt = errors.New("swap: backup integrity check failed")
	// ErrRestore wraps a rollback-restore failure (the most dangerous path).
	ErrRestore = errors.New("swap: restore failed")
)

// Layout is the fixed, install-determined set of paths the swap operates on. The
// paths come from the updater's own install dir (NOT the manifest), so a manifest
// cannot influence local file locations. StagingDir MUST be on the same volume as
// LiveBinary for the atomic rename.
type Layout struct {
	LiveBinary string // path to the live agent binary (replaced atomically)
	LiveConfig string // path to the live config; "" skips config backup/restore
	StagingDir string // same-volume, ACL-locked staging directory
}

func (l Layout) validate() error {
	if l.LiveBinary == "" {
		return fmt.Errorf("%w: empty LiveBinary", ErrLayout)
	}
	if l.StagingDir == "" {
		return fmt.Errorf("%w: empty StagingDir", ErrLayout)
	}
	return nil
}

func (l Layout) downloadTmp() string { return filepath.Join(l.StagingDir, "download.tmp") }
func (l Layout) stagedNew() string {
	return filepath.Join(l.StagingDir, filepath.Base(l.LiveBinary)+".new")
}
func (l Layout) binaryBak() string {
	return filepath.Join(l.StagingDir, filepath.Base(l.LiveBinary)+".bak")
}
func (l Layout) configBak() string {
	if l.LiveConfig == "" {
		return ""
	}
	return filepath.Join(l.StagingDir, filepath.Base(l.LiveConfig)+".bak")
}
func (l Layout) restoreTmp() string { return filepath.Join(l.StagingDir, "restore.tmp") }

// configRestoreTmp is on the CONFIG's volume (not the staging dir, which is on the
// binary's volume) so the config can be replaced by an atomic same-volume rename.
func (l Layout) configRestoreTmp() string {
	if l.LiveConfig == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(l.LiveConfig), filepath.Base(l.LiveConfig)+".restoretmp")
}

// Swapper executes the file operations of an update over a fixed Layout.
type Swapper struct {
	layout     Layout
	downloader Downloader
	hardCap    int64
}

// New builds a Swapper. downloader is the artifact source (nil uses the hardened
// default HTTPS downloader). hardCap <= 0 uses DefaultMaxArtifactBytes.
func New(layout Layout, downloader Downloader, hardCap int64) (*Swapper, error) {
	if err := layout.validate(); err != nil {
		return nil, err
	}
	if downloader == nil {
		downloader = NewHTTPDownloader(nil)
	}
	if hardCap <= 0 {
		hardCap = DefaultMaxArtifactBytes
	}
	return &Swapper{layout: layout, downloader: downloader, hardCap: hardCap}, nil
}

// Stage downloads the artifact to staging/download.tmp (bounded by
// min(art.SizeBytes, hardCap)), verifies BOTH the SHA-256 and BLAKE3 of the exact
// downloaded bytes against the SIGNED manifest artifact (T23, verify-then-exec),
// then performs a lightweight executable-format sanity check (PE/ELF — corruption
// detection only, NOT a trust mechanism) for targetOS, and finally promotes the
// verified file to <binary>.new. A failure leaves no .new file.
func (s *Swapper) Stage(ctx context.Context, art manifest.Artifact, targetOS string) error {
	// Clear any staged binary from a prior attempt FIRST, so a non-completing Stage
	// can never leave a consumable .new — Swap must only ever see the .new of the
	// most recent SUCCESSFUL Stage (no wrong-binary swap).
	if err := os.Remove(s.layout.stagedNew()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: clearing prior staged binary: %w", ErrStage, err)
	}
	limit := art.SizeBytes
	if limit <= 0 || limit > s.hardCap {
		limit = s.hardCap
	}
	tmp := s.layout.downloadTmp()
	if err := s.download(ctx, art.URL, tmp, limit); err != nil {
		_ = os.Remove(tmp)
		return err // ErrDownload / ErrSizeExceeded (already wrapped)
	}
	// Verify-then-exec on the exact staged bytes (the file that will be promoted).
	if err := s.verifyFile(art, tmp); err != nil {
		_ = os.Remove(tmp)
		return err // verify.ErrHashMismatch (wrapped) or ErrStage
	}
	if err := validateExecutableFile(targetOS, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: %w", ErrStage, err)
	}
	// Promote the verified file to .new (atomic within the staging dir).
	if err := atomicReplace(tmp, s.layout.stagedNew()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: promote: %w", ErrStage, err)
	}
	return nil
}

func (s *Swapper) download(ctx context.Context, url, dst string, maxBytes int64) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: open %s: %w", ErrDownload, dst, err)
	}
	_, derr := s.downloader.Download(ctx, url, f, maxBytes)
	if derr != nil {
		_ = f.Close()
		return derr // ErrDownload / ErrSizeExceeded (wrapped by the downloader)
	}
	if err := f.Sync(); err != nil { // durable before verify/promote
		_ = f.Close()
		return fmt.Errorf("%w: fsync: %w", ErrDownload, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w: close: %w", ErrDownload, err)
	}
	return nil
}

func (s *Swapper) verifyFile(art manifest.Artifact, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open for verify: %w", ErrStage, err)
	}
	defer func() { _ = f.Close() }()
	if err := verify.Artifact(art, f); err != nil {
		return err // verify.ErrHashMismatch (errors.Is-matchable)
	}
	return nil
}

// Backup snapshots the live binary (and config, if configured) into the staging
// dir and returns the BLAKE3 of the binary backup for the integrity-checked
// restore. It does NOT touch the live binary.
func (s *Swapper) Backup() (hashing.Digest, error) {
	bak := s.layout.binaryBak()
	if err := copyFileSync(s.layout.LiveBinary, bak); err != nil {
		return hashing.Digest{}, fmt.Errorf("%w: binary: %w", ErrBackup, err)
	}
	dg, err := hashFile(bak)
	if err != nil {
		return hashing.Digest{}, fmt.Errorf("%w: digest: %w", ErrBackup, err)
	}
	if cb := s.layout.configBak(); cb != "" {
		if err := copyFileSync(s.layout.LiveConfig, cb); err != nil {
			return hashing.Digest{}, fmt.Errorf("%w: config: %w", ErrBackup, err)
		}
	}
	return dg, nil
}

// Swap atomically replaces the live binary with the staged .new. The agent MUST be
// stopped by the caller (T27) first. An interrupted swap leaves the old OR the new
// binary, never a truncated file.
func (s *Swapper) Swap() error {
	staged := s.layout.stagedNew()
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf("%w: %v", ErrNotStaged, err)
	}
	if err := atomicReplace(staged, s.layout.LiveBinary); err != nil {
		return fmt.Errorf("%w: %w", ErrSwap, err)
	}
	return nil
}

// Restore rolls back: it copies the binary backup to a same-volume temp, verifies
// THAT COPY (the exact bytes that will be installed) against wantBinDigest (the
// value returned by Backup), then atomically renames it over the live binary; the
// config (if configured) is restored the same atomic way. A corrupt backup is never
// installed, and neither live file is ever left truncated. The .bak is kept for a
// retry.
func (s *Swapper) Restore(wantBinDigest hashing.Digest) error {
	// Stage the binary restore on the live binary's volume, then verify the copy
	// itself (closes the hash-bak / install-a-different-file gap).
	rt := s.layout.restoreTmp()
	if err := copyFileSync(s.layout.binaryBak(), rt); err != nil {
		return fmt.Errorf("%w: stage restore: %w", ErrRestore, err)
	}
	got, err := hashFile(rt)
	if err != nil {
		_ = os.Remove(rt)
		return fmt.Errorf("%w: hashing restore copy: %w", ErrRestore, err)
	}
	if got.String() != wantBinDigest.String() {
		_ = os.Remove(rt)
		return fmt.Errorf("%w: backup %s != recorded %s", ErrBackupCorrupt, got.String(), wantBinDigest.String())
	}
	if err := atomicReplace(rt, s.layout.LiveBinary); err != nil {
		_ = os.Remove(rt)
		return fmt.Errorf("%w: binary: %w", ErrRestore, err)
	}
	// Config restore: atomic too — copy to a same-volume temp (the config's dir),
	// then rename, so the live config is never truncated in place.
	if cb := s.layout.configBak(); cb != "" {
		ct := s.layout.configRestoreTmp()
		if err := copyFileSync(cb, ct); err != nil {
			_ = os.Remove(ct)
			return fmt.Errorf("%w: stage config restore: %w", ErrRestore, err)
		}
		if err := atomicReplace(ct, s.layout.LiveConfig); err != nil {
			_ = os.Remove(ct)
			return fmt.Errorf("%w: config: %w", ErrRestore, err)
		}
	}
	return nil
}

// Commit removes the staged and backup artifacts after a successful health gate.
// It is best-effort (errors are joined but non-fatal to the committed state).
func (s *Swapper) Commit() error {
	var errs []error
	for _, p := range []string{
		s.layout.stagedNew(), s.layout.binaryBak(), s.layout.configBak(),
		s.layout.downloadTmp(), s.layout.restoreTmp(), s.layout.configRestoreTmp(),
	} {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// hashFile returns the BLAKE3 digest of the file at path (streaming).
func hashFile(path string) (hashing.Digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return hashing.Digest{}, err
	}
	defer func() { _ = f.Close() }()
	return hashing.HashReader(hashing.BLAKE3, f)
}

// copyFileSync copies src to dst (0600) and fsyncs dst.
func copyFileSync(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
