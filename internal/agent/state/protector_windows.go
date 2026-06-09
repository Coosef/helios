//go:build windows

package state

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultProtector on Windows wraps secrets with DPAPI machine-scope
// (CRYPTPROTECT_LOCAL_MACHINE), so any process on this machine running as
// SYSTEM/Administrator can unwrap them — which is why the directory ACL, not
// DPAPI, is the real access boundary (§0.4). DPAPI is defense-in-depth against
// offline disk theft.
func defaultProtector() Protector { return dpapiProtector{} }

type dpapiProtector struct{}

func (dpapiProtector) Name() string { return "dpapi-machine" }

func (dpapiProtector) Protect(plaintext []byte) ([]byte, error) {
	return dpapiCrypt(plaintext, true)
}

func (dpapiProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	return dpapiCrypt(ciphertext, false)
}

var (
	crypt32       = windows.NewLazySystemDLL("crypt32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprotect = crypt32.NewProc("CryptUnprotectData")
	kernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree = kernel32.NewProc("LocalFree")
)

const cryptProtectLocalMachine = 0x4 // CRYPTPROTECT_LOCAL_MACHINE

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func dpapiCrypt(in []byte, encrypt bool) ([]byte, error) {
	var inBlob dataBlob
	if len(in) > 0 {
		inBlob = dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	}
	var out dataBlob

	proc := procUnprotect
	op := "CryptUnprotectData"
	if encrypt {
		proc, op = procProtect, "CryptProtectData"
	}

	// BOOL Crypt(Un)ProtectData(pDataIn, szDataDescr, pOptionalEntropy,
	//   pvReserved, pPromptStruct, dwFlags, pDataOut)
	r, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&inBlob)), // #nosec G103 -- required Win32 DPAPI interop
		0, 0, 0, 0,
		uintptr(cryptProtectLocalMachine),
		uintptr(unsafe.Pointer(&out)), // #nosec G103
	)
	if r == 0 {
		return nil, fmt.Errorf("state: dpapi %s failed: %w", op, callErr)
	}
	defer func() {
		_, _, _ = procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData))) // #nosec G103
	}()

	res := make([]byte, out.cbData)
	if out.cbData > 0 {
		copy(res, unsafe.Slice(out.pbData, out.cbData)) // #nosec G103
	}
	return res, nil
}
