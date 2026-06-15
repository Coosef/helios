//go:build linux

package service

import (
	"net"
	"os"
)

// notifyReady sends the systemd sd_notify "READY=1" datagram when the process runs
// under a Type=notify unit (i.e. $NOTIFY_SOCKET is set). It is best-effort and
// Linux-only: any error is ignored, because a missing, abstract, or unwritable
// notify socket must never crash or delay the agent. It is a no-op outside systemd.
//
// Implemented with the standard library only (no systemd dependency): a connected
// AF_UNIX datagram socket to $NOTIFY_SOCKET, with the abstract-namespace form
// (leading '@') mapped to a leading NUL per the sd_notify protocol.
func notifyReady() {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return // not launched under a Type=notify systemd unit
	}
	if socket[0] == '@' {
		socket = "\x00" + socket[1:] // abstract socket namespace
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write([]byte("READY=1"))
}
