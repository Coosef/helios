// Package mocksaas is TEST TOOLING (S1-T28): it generates deterministic, signed
// update-manifest + artifact fixtures and serves them, so the updater (T22–T27),
// CI (T31), and integration (T34) can exercise the real verify/decide/swap path
// offline without a live SaaS.
//
// It is NOT a product backend: no auth, no tenant/RBAC, no storage, no UI, no
// stateful token store. The control plane is mocked separately by Prism from
// api/openapi.yaml (see the Taskfile `contract`/`spec:mock` targets).
//
// SECURITY: fixtures are signed by a DETERMINISTIC TEST key generated at runtime
// from a fixed seed (trusttest). The private key is NEVER written to disk — only
// the public keyset, the signed manifests, and the artifacts are committed
// (AC-35). Updater tests MUST verify these against TestKeySet(), NEVER against the
// embedded production keyset (trust.Embedded()), whose private half was discarded.
package mocksaas

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zeebo/blake3"

	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust/trusttest"
	"github.com/beyzbackup/beyz-backup/internal/updater/verify"
)

// Stable identifiers + the path the updater fetches the manifest from.
const (
	TestKeyID    = "beyz-test-2026-01"
	RevokedKeyID = "beyz-test-revoked"
	ManifestPath = "/v1/updates/manifest"
	artifactPath = "/artifacts" // <ManifestPath host>/artifacts/<case>.bin

	// releasedAt is a fixed timestamp so fixtures are byte-deterministic.
	releasedAt = "2026-01-15T00:00:00Z"
)

// seeds are deterministic; the derived private keys exist only in memory.
var (
	activeSeed  = trusttest.SeedFromByte(0x2a)
	revokedSeed = trusttest.SeedFromByte(0x2b)
)

func activeKeys() (ed25519.PublicKey, ed25519.PrivateKey) {
	return trusttest.DeterministicKeyPair(activeSeed)
}
func revokedKeys() (ed25519.PublicKey, ed25519.PrivateKey) {
	return trusttest.DeterministicKeyPair(revokedSeed)
}

// TestKeySet is the trust set updater tests MUST inject (never trust.Embedded()).
// Both keys are ACTIVE in the set; the revoked-key fixture self-revokes its signing
// key via the manifest's own key_revocation_list (the realistic recall path).
func TestKeySet() *trust.KeySet {
	activePub, _ := activeKeys()
	revokedPub, _ := revokedKeys()
	ks, err := trusttest.KeySet(
		trust.KeyEntry{KeyID: TestKeyID, PublicKey: activePub},
		trust.KeyEntry{KeyID: RevokedKeyID, PublicKey: revokedPub},
	)
	if err != nil {
		panic("mocksaas: building test keyset: " + err.Error())
	}
	return ks
}

// PublicKeySetJSON returns the committed public keyset (keyset.json shape) — public
// material only, safe to commit.
func PublicKeySetJSON() ([]byte, error) {
	activePub, _ := activeKeys()
	revokedPub, _ := revokedKeys()
	type entry struct {
		KeyID     string `json:"key_id"`
		PublicKey string `json:"public_key"` // base64 raw Ed25519
		Algorithm string `json:"algorithm"`
		Revoked   bool   `json:"revoked"`
	}
	doc := map[string]any{
		"schema_version": 1,
		"keys": []entry{
			{TestKeyID, b64(activePub), "ed25519", false},
			{RevokedKeyID, b64(revokedPub), "ed25519", false},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// Fixture is one deterministic update scenario.
type Fixture struct {
	Name     string
	Manifest []byte // signed manifest JSON (or tampered-after-signing)
	Artifact []byte // the binary bytes the artifact URL serves
	Platform string // platform/arch to evaluate the decision for
	Arch     string
	Baseline string // the updater's current-version baseline for the decision

	// Expected outcomes (for the table-driven test).
	ExpectProceed bool
	ExpectReason  string // manifestcheck reason when !ExpectProceed
	// ExpectArtifactErr, when non-nil, is the error verify.Artifact must return for
	// the served bytes (the hash-mismatch case). nil means the artifact verifies.
	ExpectArtifactErr error
}

// elfArtifact returns deterministic bytes with a valid ELF magic header so the
// valid fixture passes T25's PE/ELF sanity gate.
func elfArtifact(marker string, n int) []byte {
	b := make([]byte, n)
	b[0], b[1], b[2], b[3] = 0x7F, 'E', 'L', 'F'
	copy(b[4:], marker)
	for i := 4 + len(marker); i < n; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func sha256hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func blake3hex(b []byte) string { s := blake3.Sum256(b); return hex.EncodeToString(s[:]) }
func b64(b []byte) string       { return base64.StdEncoding.EncodeToString(b) }

// artifactFor builds the manifest artifact metadata whose hashes match data.
func artifactFor(platform, arch, url string, data []byte) map[string]any {
	return map[string]any{
		"platform": platform, "arch": arch, "url": url,
		"size_bytes": len(data), "sha256": sha256hex(data), "blake3": blake3hex(data),
	}
}

// manifestMap builds a complete, schema-valid manifest with one artifact.
func manifestMap(target, minSupported string, artifact map[string]any, mutate func(map[string]any)) map[string]any {
	m := map[string]any{
		"schema_version":        1,
		"target_version":        target,
		"min_supported_version": minSupported,
		"released_at":           releasedAt,
		"artifacts":             []any{artifact},
		"key_id":                TestKeyID,
		"key_revocation_list":   []any{},
		"signature":             "", // overwritten by SignManifest
	}
	if mutate != nil {
		mutate(m)
	}
	return m
}

func sign(m map[string]any, priv ed25519.PrivateKey) ([]byte, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return trusttest.SignManifest(raw, priv)
}

// Generate returns the seven deterministic fixtures. It panics on an internal
// inconsistency (a generator bug, not a runtime condition).
func Generate() []Fixture {
	_, activePriv := activeKeys()
	_, revokedPriv := revokedKeys()
	// A canonical https placeholder so the signed manifest passes manifest.Validate.
	// The local Server serves the same bytes at its own path; tests resolve the real
	// download URL via Server.ArtifactURL(), never this placeholder.
	url := func(name string) string { return "https://updates.beyz.test" + artifactPath + "/" + name + ".bin" }
	must := func(b []byte, err error) []byte {
		if err != nil {
			panic("mocksaas: generating fixture: " + err.Error())
		}
		return b
	}

	var fx []Fixture

	// 1. valid — proceeds; artifact verifies; passes the ELF gate.
	validArt := elfArtifact("VALID", 4096)
	validMan := must(sign(manifestMap("1.2.0", "1.0.0", artifactFor("linux", "amd64", url("valid"), validArt), nil), activePriv))
	fx = append(fx, Fixture{
		Name: "valid", Manifest: validMan, Artifact: validArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0", ExpectProceed: true, ExpectReason: "ok",
	})

	// 2. invalid-signature — sign validly, then tamper a SIGNED field.
	badSig := tamperTargetVersion(validMan, "9.9.9")
	fx = append(fx, Fixture{
		Name: "invalid-signature", Manifest: badSig, Artifact: validArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: false, ExpectReason: "manifest_rejected",
	})

	// 3. revoked-key — signed by the revoked key, which the manifest self-revokes.
	revArt := elfArtifact("REVOKED", 4096)
	revMan := must(sign(manifestMap("1.2.0", "1.0.0", artifactFor("linux", "amd64", url("revoked-key"), revArt), func(m map[string]any) {
		m["key_id"] = RevokedKeyID
		m["key_revocation_list"] = []any{RevokedKeyID}
	}), revokedPriv))
	fx = append(fx, Fixture{
		Name: "revoked-key", Manifest: revMan, Artifact: revArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: false, ExpectReason: "manifest_rejected",
	})

	// 4. hash-mismatch — VALID manifest, but the served artifact bytes do not match
	//    the manifest hashes (the decision proceeds; verify.Artifact must fail).
	declared := elfArtifact("DECLARED", 4096)
	servedWrong := elfArtifact("WRONG", 4096)
	mmMan := must(sign(manifestMap("1.2.0", "1.0.0", artifactFor("linux", "amd64", url("hash-mismatch"), declared), nil), activePriv))
	fx = append(fx, Fixture{
		Name: "hash-mismatch", Manifest: mmMan, Artifact: servedWrong, // <-- mismatched on purpose
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: true, ExpectReason: "ok", ExpectArtifactErr: verify.ErrHashMismatch,
	})

	// 5. downgrade-blocked — target <= baseline (and >= min, so it is purely a downgrade).
	dgArt := elfArtifact("DOWNGRADE", 4096)
	dgMan := must(sign(manifestMap("1.0.0", "0.9.0", artifactFor("linux", "amd64", url("downgrade-blocked"), dgArt), nil), activePriv))
	fx = append(fx, Fixture{
		Name: "downgrade-blocked", Manifest: dgMan, Artifact: dgArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: false, ExpectReason: "downgrade_blocked",
	})

	// 6. update-not-allowed — signed kill-switch update_allowed=false.
	naArt := elfArtifact("KILLSWITCH", 4096)
	naMan := must(sign(manifestMap("1.2.0", "1.0.0", artifactFor("linux", "amd64", url("update-not-allowed"), naArt), func(m map[string]any) {
		m["update_allowed"] = false
	}), activePriv))
	fx = append(fx, Fixture{
		Name: "update-not-allowed", Manifest: naMan, Artifact: naArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: false, ExpectReason: "update_not_allowed",
	})

	// 7. no-artifact — a schema-valid manifest whose only artifact is for a DIFFERENT
	//    (valid) platform/arch than the one we evaluate for → ErrNoArtifact.
	otherArt := elfArtifact("OTHERARCH", 4096)
	naf := must(sign(manifestMap("1.2.0", "1.0.0", artifactFor("windows", "arm64", url("no-artifact"), otherArt), nil), activePriv))
	fx = append(fx, Fixture{
		Name: "no-artifact", Manifest: naf, Artifact: otherArt,
		Platform: "linux", Arch: "amd64", Baseline: "1.1.0",
		ExpectProceed: false, ExpectReason: "no_artifact",
	})

	return fx
}

// tamperTargetVersion rewrites target_version in an already-signed manifest, so the
// signature (which covered the original value) no longer verifies.
func tamperTargetVersion(signed []byte, newTarget string) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(signed, &obj); err != nil {
		panic("mocksaas: tamper: " + err.Error())
	}
	enc, _ := json.Marshal(newTarget)
	obj["target_version"] = enc
	out, err := json.Marshal(obj)
	if err != nil {
		panic("mocksaas: tamper marshal: " + err.Error())
	}
	return out
}

// ByName returns the fixture with the given name (panics if unknown — test helper).
func ByName(name string) Fixture {
	for _, f := range Generate() {
		if f.Name == name {
			return f
		}
	}
	panic(fmt.Sprintf("mocksaas: unknown fixture %q", name))
}
