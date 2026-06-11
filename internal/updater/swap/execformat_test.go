package swap

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func validPE() []byte {
	b := make([]byte, 0x80)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3C:0x40], 0x40) // e_lfanew
	b[0x40], b[0x41], b[0x42], b[0x43] = 'P', 'E', 0, 0
	return b
}

func validELF() []byte {
	b := make([]byte, 64)
	b[0], b[1], b[2], b[3] = 0x7F, 'E', 'L', 'F'
	return b
}

func TestValidateExecutableFile(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		data    []byte
		wantErr bool
	}{
		{"valid PE", "windows", validPE(), false},
		{"PE missing MZ", "windows", []byte("XX............................................................PE\x00\x00"), true},
		{"PE missing sig", "windows", func() []byte { b := validPE(); b[0x40] = 'X'; return b }(), true},
		{"ELF on windows target", "windows", validELF(), true},
		{"valid ELF", "linux", validELF(), false},
		{"ELF bad magic", "linux", []byte("\x7FELG and more bytes here............"), true},
		{"PE on linux target", "linux", validPE(), true},
		{"plain text on linux", "linux", []byte("not an executable at all, just text"), true},
		{"unknown target skips", "darwin", []byte("anything"), false},
		{"truncated", "linux", []byte{0x7F}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateExecutableFile(c.target, writeTmp(t, c.data))
			if c.wantErr && !errors.Is(err, ErrNotExecutable) {
				t.Errorf("err = %v, want ErrNotExecutable", err)
			}
			if !c.wantErr && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}
