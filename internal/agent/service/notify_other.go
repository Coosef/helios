//go:build !linux

package service

// notifyReady is a no-op on non-Linux platforms (systemd sd_notify is Linux-only).
func notifyReady() {}
