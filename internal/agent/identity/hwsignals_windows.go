//go:build windows

package identity

import "golang.org/x/sys/windows/registry"

// rawMachineID reads the Windows MachineGuid (advisory clone-detection signal).
func rawMachineID() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return v
}

// rawDiskSerial is an advisory slot, not collected on Windows in Sprint 1.
func rawDiskSerial() string { return "" }

// osName returns the OpenAPI-enum-valid OS name.
func osName() string { return "windows" }

// osVersion returns the Windows build number (advisory), or "".
func osVersion() string {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	if v, _, err := k.GetStringValue("CurrentBuild"); err == nil {
		return v
	}
	return ""
}
