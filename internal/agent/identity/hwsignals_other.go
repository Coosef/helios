//go:build !windows && !linux

package identity

import (
	"os"
	"strings"
)

// rawMachineID is best-effort on other platforms (e.g. macOS dev hosts): reads
// /etc/machine-id if present, otherwise "".
func rawMachineID() string {
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// rawDiskSerial is not collected on these platforms in Sprint 1.
func rawDiskSerial() string { return "" }

// osName returns "" on unsupported (non-windows/linux) dev hosts so the OpenAPI
// os enum [windows, linux] is never violated (omitempty drops the field).
func osName() string { return "" }

// osVersion is not collected on these platforms.
func osVersion() string { return "" }
