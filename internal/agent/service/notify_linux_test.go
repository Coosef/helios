//go:build linux

package service

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// notifyReady must deliver exactly "READY=1" to $NOTIFY_SOCKET (the systemd
// Type=notify readiness signal).
func TestNotifyReadySendsReady(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	srv, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = srv.Close() }()
	t.Setenv("NOTIFY_SOCKET", sock)

	notifyReady()

	_ = srv.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _, err := srv.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Errorf("notify payload = %q, want %q", got, "READY=1")
	}
}

// With no notify socket configured, notifyReady is a no-op (must not panic/block/error).
func TestNotifyReadyNoSocketIsNoop(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	notifyReady()
}

// A non-existent notify socket fails silently (best-effort: a broken socket must
// never crash or delay the agent).
func TestNotifyReadyBadSocketIsSilent(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", filepath.Join(t.TempDir(), "does-not-exist.sock"))
	notifyReady()
}
