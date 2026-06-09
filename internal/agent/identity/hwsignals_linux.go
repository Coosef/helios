//go:build linux

package identity

import (
	"os"
	"path/filepath"
	"strings"
)

// rawMachineID reads /etc/machine-id (or the dbus fallback).
func rawMachineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	return ""
}

// rawDiskSerial reads a non-virtual block device's serial from sysfs (best-effort).
func rawDiskSerial() string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "dm-") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/sys/block", name, "device", "serial")); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return ""
}

// osName returns the OpenAPI-enum-valid OS name.
func osName() string { return "linux" }

// osVersion returns VERSION_ID from /etc/os-release (advisory), or "".
func osVersion() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "VERSION_ID="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
