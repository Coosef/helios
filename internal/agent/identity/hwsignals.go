package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
)

// HardwareSignals holds advisory, privacy-preserving clone-detection signals
// (ADR-003, LIC-2). Identifying values (machine GUID, disk serial, NIC MAC) are
// stored ONLY as deterministic "sha256:<hex>" hashes — the raw values never leave
// the collector. These are advisory only, never the primary identity or license
// key. Mirrors the OpenAPI HardwareSignals schema.
type HardwareSignals struct {
	MachineGUIDSHA256       string `json:"machine_guid_sha256,omitempty"`
	PrimaryDiskSerialSHA256 string `json:"primary_disk_serial_sha256,omitempty"`
	FirstNICMACSHA256       string `json:"first_nic_mac_sha256,omitempty"`
	OS                      string `json:"os,omitempty"`
	OSVersion               string `json:"os_version,omitempty"`
}

// hashSignal returns "sha256:<hex>" of a raw signal, or "" when raw is empty (so
// an unavailable signal is omitted, never emitted raw).
func hashSignal(raw string) string {
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CollectHardwareSignals gathers advisory signals and returns them HASHED. Raw
// identifying values are confined to this call and never returned or logged.
func CollectHardwareSignals() HardwareSignals {
	return HardwareSignals{
		MachineGUIDSHA256:       hashSignal(rawMachineID()),
		PrimaryDiskSerialSHA256: hashSignal(rawDiskSerial()),
		FirstNICMACSHA256:       hashSignal(rawFirstNICMAC()),
		// osName() returns only a value permitted by the OpenAPI enum
		// (windows|linux), or "" on unsupported dev hosts so omitempty drops it.
		OS:        osName(),
		OSVersion: osVersion(),
	}
}

// rawFirstNICMAC returns the first non-loopback interface's MAC, or "".
func rawFirstNICMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if mac := ifi.HardwareAddr.String(); mac != "" {
			return mac
		}
	}
	return ""
}
