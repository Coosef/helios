package hashing_test

import (
	"crypto/sha512"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/beyzbackup/beyz-backup/pkg/hashing"
)

// Known-answer vectors (independent references).
const (
	sha256Empty = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sha256ABC   = "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	blake3Empty = "blake3:af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"
	sha512Empty = "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"
)

func mustHash(t *testing.T, algo hashing.Algorithm, data []byte) hashing.Digest {
	t.Helper()
	d, err := hashing.HashBytes(algo, data)
	if err != nil {
		t.Fatalf("HashBytes(%s): %v", algo, err)
	}
	return d
}

func TestKnownAnswerVectors(t *testing.T) {
	cases := []struct {
		algo hashing.Algorithm
		in   string
		want string
	}{
		{hashing.SHA256, "", sha256Empty},
		{hashing.SHA256, "abc", sha256ABC},
		{hashing.BLAKE3, "", blake3Empty},
	}
	for _, c := range cases {
		got := mustHash(t, c.algo, []byte(c.in)).String()
		if got != c.want {
			t.Errorf("%s(%q) = %s, want %s", c.algo, c.in, got, c.want)
		}
	}
}

func TestDefaultAlgorithmIsBLAKE3(t *testing.T) {
	if hashing.DefaultAlgorithm != hashing.BLAKE3 {
		t.Errorf("DefaultAlgorithm = %s, want blake3", hashing.DefaultAlgorithm)
	}
}

func TestTaggedFormatAndAccessors(t *testing.T) {
	d := mustHash(t, hashing.SHA256, []byte("abc"))
	if d.Algorithm() != hashing.SHA256 {
		t.Errorf("Algorithm() = %s, want sha256", d.Algorithm())
	}
	if len(d.HexDigest()) != 64 {
		t.Errorf("HexDigest() length = %d, want 64", len(d.HexDigest()))
	}
	if d.String() != "sha256:"+d.HexDigest() {
		t.Errorf("String() = %q, not tagged", d.String())
	}
	if got := d.Bytes(); len(got) != 32 {
		t.Errorf("Bytes() length = %d, want 32", len(got))
	}
	// Bytes returns a copy: mutating it must not affect the digest.
	b := d.Bytes()
	b[0] ^= 0xFF
	if d.Bytes()[0] == b[0] {
		t.Error("Bytes() did not return a defensive copy")
	}
	// Round-trip through ParseDigest.
	p, err := hashing.ParseDigest(d.String())
	if err != nil {
		t.Fatalf("ParseDigest round-trip: %v", err)
	}
	if p.String() != d.String() {
		t.Errorf("round-trip mismatch: %s vs %s", p.String(), d.String())
	}
}

func TestHashBytesReaderFileConsistency(t *testing.T) {
	content := []byte("the quick brown fox jumps over the lazy dog")
	dir := t.TempDir()
	path := filepath.Join(dir, "content.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, algo := range []hashing.Algorithm{hashing.BLAKE3, hashing.SHA256} {
		fromBytes := mustHash(t, algo, content).String()

		fromReader, err := hashing.HashReader(algo, bytesReader(content))
		if err != nil {
			t.Fatalf("HashReader: %v", err)
		}
		fromFile, err := hashing.HashFile(algo, path)
		if err != nil {
			t.Fatalf("HashFile: %v", err)
		}
		if fromReader.String() != fromBytes || fromFile.String() != fromBytes {
			t.Errorf("%s: inconsistent digests bytes=%s reader=%s file=%s",
				algo, fromBytes, fromReader.String(), fromFile.String())
		}
	}
}

func TestVerifySuccessAndFailure(t *testing.T) {
	content := []byte("verify me")
	dir := t.TempDir()
	path := filepath.Join(dir, "v.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	tagged := mustHash(t, hashing.BLAKE3, content).String()

	if err := hashing.VerifyBytes(tagged, content); err != nil {
		t.Errorf("VerifyBytes success path: %v", err)
	}
	if err := hashing.VerifyReader(tagged, bytesReader(content)); err != nil {
		t.Errorf("VerifyReader success path: %v", err)
	}
	if err := hashing.VerifyFile(tagged, path); err != nil {
		t.Errorf("VerifyFile success path: %v", err)
	}

	tampered := []byte("verify ME")
	if err := hashing.VerifyBytes(tagged, tampered); !errors.Is(err, hashing.ErrHashMismatch) {
		t.Errorf("VerifyBytes(tampered) error = %v, want ErrHashMismatch", err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hashing.VerifyFile(tagged, path); !errors.Is(err, hashing.ErrHashMismatch) {
		t.Errorf("VerifyFile(tampered) error = %v, want ErrHashMismatch", err)
	}
}

func TestParseDigestErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", hashing.ErrMalformedDigest},
		{"no prefix", "deadbeef", hashing.ErrMalformedDigest},
		{"algo no colon", "blake3", hashing.ErrMalformedDigest},
		{"empty algo", ":deadbeef", hashing.ErrMalformedDigest},
		{"empty hex", "blake3:", hashing.ErrMalformedDigest},
		{"invalid hex", "sha256:zzzz", hashing.ErrMalformedDigest},
		{"uppercase hex", "sha256:E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855", hashing.ErrMalformedDigest},
		{"wrong length", "sha256:dead", hashing.ErrMalformedDigest},
		{"unknown algo", "md5:5d41402abc4b2a76b9719d911017c592", hashing.ErrUnsupportedAlgorithm},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := hashing.ParseDigest(c.in)
			if !errors.Is(err, c.want) {
				t.Errorf("ParseDigest(%q) error = %v, want %v", c.in, err, c.want)
			}
		})
	}

	// A valid digest must parse.
	if _, err := hashing.ParseDigest(sha256Empty); err != nil {
		t.Errorf("ParseDigest(valid) unexpected error: %v", err)
	}
}

func TestHashUnsupportedAlgorithm(t *testing.T) {
	const md5 = hashing.Algorithm("md5")
	if _, err := hashing.HashBytes(md5, []byte("x")); !errors.Is(err, hashing.ErrUnsupportedAlgorithm) {
		t.Errorf("HashBytes(md5) error = %v, want ErrUnsupportedAlgorithm", err)
	}
	if _, err := hashing.HashReader(md5, bytesReader([]byte("x"))); !errors.Is(err, hashing.ErrUnsupportedAlgorithm) {
		t.Errorf("HashReader(md5) error = %v, want ErrUnsupportedAlgorithm", err)
	}
	if !hashing.IsSupportedAlgorithm(hashing.BLAKE3) || hashing.IsSupportedAlgorithm(md5) {
		t.Error("IsSupportedAlgorithm gave wrong result")
	}
}

func TestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := hashing.HashFile(hashing.SHA256, path); err != nil || d.String() != sha256Empty {
		t.Errorf("HashFile(empty, sha256) = %s, %v; want %s", d.String(), err, sha256Empty)
	}
	if d, err := hashing.HashFile(hashing.BLAKE3, path); err != nil || d.String() != blake3Empty {
		t.Errorf("HashFile(empty, blake3) = %s, %v; want %s", d.String(), err, blake3Empty)
	}
}

func TestLargeStreamingHashAndVerify(t *testing.T) {
	const size = 8 << 20 // 8 MiB, hashed via a streaming reader (never held in memory)
	makeReader := func() io.Reader { return io.LimitReader(constReader('x'), size) }

	d, err := hashing.HashReader(hashing.BLAKE3, makeReader())
	if err != nil {
		t.Fatalf("HashReader(large): %v", err)
	}
	if err := hashing.VerifyReader(d.String(), makeReader()); err != nil {
		t.Errorf("VerifyReader(large) round-trip: %v", err)
	}
	// A 1-byte difference must be detected.
	bad := io.MultiReader(io.LimitReader(constReader('x'), size-1), constReader('y'))
	if err := hashing.VerifyReader(d.String(), io.LimitReader(bad, size)); !errors.Is(err, hashing.ErrHashMismatch) {
		t.Errorf("VerifyReader(altered large) = %v, want ErrHashMismatch", err)
	}
}

func TestReaderError(t *testing.T) {
	if _, err := hashing.HashReader(hashing.BLAKE3, errReader{}); err == nil || errors.Is(err, hashing.ErrHashMismatch) {
		t.Errorf("HashReader(errReader) error = %v, want a read error", err)
	}
	if err := hashing.VerifyReader(blake3Empty, errReader{}); err == nil {
		t.Error("VerifyReader(errReader) should error")
	}
}

func TestFileOpenErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := hashing.HashFile(hashing.BLAKE3, missing); err == nil {
		t.Error("HashFile(missing) should error")
	}
	if err := hashing.VerifyFile(blake3Empty, missing); err == nil {
		t.Error("VerifyFile(missing) should error")
	}
}

// Demonstrates that a future algorithm (here SHA-512) can be added via Register
// with no change to the tagged format, and immediately works end to end.
func TestFutureAlgorithmExtensibility(t *testing.T) {
	const sha512Algo = hashing.Algorithm("sha512")

	if hashing.IsSupportedAlgorithm(sha512Algo) {
		t.Skip("sha512 already registered by another test")
	}
	hashing.Register(sha512Algo, sha512.New)

	if !hashing.IsSupportedAlgorithm(sha512Algo) {
		t.Fatal("sha512 not registered")
	}
	if got := mustHash(t, sha512Algo, []byte("")).String(); got != sha512Empty {
		t.Errorf("sha512('') = %s, want %s", got, sha512Empty)
	}
	tagged := mustHash(t, sha512Algo, []byte("future")).String()
	if err := hashing.VerifyBytes(tagged, []byte("future")); err != nil {
		t.Errorf("VerifyBytes(sha512) success path: %v", err)
	}
	found := false
	for _, a := range hashing.SupportedAlgorithms() {
		if a == sha512Algo {
			found = true
		}
	}
	if !found {
		t.Error("SupportedAlgorithms() does not include the newly registered sha512")
	}
}

// --- test helpers ---

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func bytesReader(b []byte) io.Reader {
	i := 0
	return readerFunc(func(p []byte) (int, error) {
		if i >= len(b) {
			return 0, io.EOF
		}
		n := copy(p, b[i:])
		i += n
		return n, nil
	})
}

// constReader is an infinite reader that yields a single repeating byte.
type constReader byte

func (c constReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(c)
	}
	return len(p), nil
}

var errBoom = errors.New("boom")

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errBoom }
