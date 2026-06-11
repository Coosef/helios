//go:build windows

package swap

import "golang.org/x/sys/windows"

// atomicReplace atomically replaces dst with src using MoveFileEx with
// REPLACE_EXISTING (atomic rename over an existing file) and WRITE_THROUGH (flush
// to disk before returning) — the durable, atomic agent-binary swap (ADR-002 #8).
func atomicReplace(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
