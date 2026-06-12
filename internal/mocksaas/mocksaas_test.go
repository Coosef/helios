package mocksaas_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/internal/mocksaas"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

// The seven fixtures drive the REAL verify/decide path to their expected outcomes,
// verified against the INJECTED test keyset (never trust.Embedded()).
func TestFixtureDecisionMatrix(t *testing.T) {
	keys := mocksaas.TestKeySet()
	for _, f := range mocksaas.Generate() {
		t.Run(f.Name, func(t *testing.T) {
			bl, err := manifest.ParseVersion(f.Baseline)
			if err != nil {
				t.Fatal(err)
			}
			dec, derr := manifestcheck.Evaluate(f.Manifest, keys, bl, f.Platform, f.Arch)
			if dec.Proceed != f.ExpectProceed {
				t.Fatalf("Proceed = %v, want %v (err=%v)", dec.Proceed, f.ExpectProceed, derr)
			}
			if dec.Reason != f.ExpectReason {
				t.Errorf("Reason = %q, want %q", dec.Reason, f.ExpectReason)
			}
			if !f.ExpectProceed {
				return
			}
			// Proceeding fixtures: check the artifact against the verified manifest.
			artErr := verify.Artifact(dec.Artifact, bytes.NewReader(f.Artifact))
			if f.ExpectArtifactErr == nil && artErr != nil {
				t.Errorf("verify.Artifact = %v, want nil", artErr)
			}
			if f.ExpectArtifactErr != nil && !errors.Is(artErr, f.ExpectArtifactErr) {
				t.Errorf("verify.Artifact = %v, want %v", artErr, f.ExpectArtifactErr)
			}
		})
	}
}

// The static server serves the manifest + artifact over HTTP, and the served bytes
// drive the same decision.
func TestServerServesManifestAndArtifact(t *testing.T) {
	s := mocksaas.NewServer("valid")
	defer s.Close()

	manRaw := httpGet(t, s.Client(), s.ManifestURL())
	if !bytes.Equal(manRaw, mocksaas.ByName("valid").Manifest) {
		t.Fatal("served manifest != fixture manifest")
	}
	bl, _ := manifest.ParseVersion("1.1.0")
	dec, err := manifestcheck.Evaluate(manRaw, mocksaas.TestKeySet(), bl, "linux", "amd64")
	if err != nil || !dec.Proceed {
		t.Fatalf("served manifest must proceed: %v / %+v", err, dec)
	}
	artRaw := httpGet(t, s.Client(), s.ArtifactURL())
	if err := verify.Artifact(dec.Artifact, bytes.NewReader(artRaw)); err != nil {
		t.Errorf("served artifact must verify: %v", err)
	}
}

// The valid artifact is swap-ready end-to-end: download (T25 HTTPDownloader) +
// dual-hash verify + the PE/ELF sanity gate, all via swap.Stage.
func TestValidArtifactPassesSwapStage(t *testing.T) {
	s := mocksaas.NewServer("valid")
	defer s.Close()

	art, ok := s.Artifact("linux", "amd64") // URL already resolved to the live server
	if !ok {
		t.Fatal("valid manifest must have a linux/amd64 artifact")
	}

	dir := t.TempDir()
	staging := dir + "/staging"
	if err := makeDir(staging); err != nil {
		t.Fatal(err)
	}
	live := dir + "/beyz-backup-agent"
	if err := writeFile(live, []byte("OLD")); err != nil {
		t.Fatal(err)
	}
	sw, err := swap.New(swap.Layout{LiveBinary: live, StagingDir: staging}, swap.NewHTTPDownloader(s.Client()), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Stage(context.Background(), art, "linux"); err != nil {
		t.Fatalf("valid artifact must Stage (download+hash+ELF gate): %v", err)
	}
}

func TestFixturesDeterministic(t *testing.T) {
	a, b := mocksaas.Generate(), mocksaas.Generate()
	if len(a) != 7 || len(b) != 7 {
		t.Fatalf("want 7 fixtures, got %d", len(a))
	}
	for i := range a {
		if a[i].Name != b[i].Name || !bytes.Equal(a[i].Manifest, b[i].Manifest) || !bytes.Equal(a[i].Artifact, b[i].Artifact) {
			t.Errorf("fixture %s is not deterministic", a[i].Name)
		}
	}
}

func httpGet(t *testing.T, c *http.Client, url string) []byte {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return b
}

// TestCommittedFixturesMatchGenerator does a FULL-TREE comparison: it regenerates
// the whole fixture set into a temp dir and asserts every committed file (manifests,
// artifacts, keyset.json, index.json) is byte-identical. This catches drift in ANY
// committed file, not just the manifests/artifacts.
func TestCommittedFixturesMatchGenerator(t *testing.T) {
	committed := filepath.Join("..", "..", "test", "fixtures")
	if _, err := os.Stat(committed); err != nil {
		t.Skipf("fixtures not present (run: go run ./cmd/mkfixtures): %v", err)
	}
	fresh := t.TempDir()
	if err := mocksaas.WriteFixtures(fresh); err != nil {
		t.Fatal(err)
	}
	// Every freshly-generated file must exist + match in the committed tree.
	err := filepath.WalkDir(fresh, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(fresh, path)
		want, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		got, gerr := os.ReadFile(filepath.Join(committed, rel))
		if gerr != nil {
			t.Errorf("%s missing/unreadable in committed tree (run: go run ./cmd/mkfixtures): %v", rel, gerr)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s drift — run: go run ./cmd/mkfixtures", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// And the committed tree must not have EXTRA files the generator would not write.
	_ = filepath.WalkDir(committed, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(committed, path)
		if _, serr := os.Stat(filepath.Join(fresh, rel)); os.IsNotExist(serr) {
			t.Errorf("%s is committed but not produced by the generator (stale) — run: go run ./cmd/mkfixtures", rel)
		}
		return nil
	})
}
