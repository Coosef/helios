package service

import "context"

// This file provides NAME-ONLY service control for EXTERNAL processes — the
// on-demand updater (S1-T27) must Stop/Start/Status the already-installed
// BeyzBackupAgent service without owning the agent's Runnable. kardianos
// Control/Status are name-addressed SCM/systemd operations, so a throwaway handle
// built with a no-op Runnable is sufficient; no second service is registered.

// noopRunnable is a do-nothing Runnable used only to construct a name handle for
// controlling/querying an already-installed service from another process.
type noopRunnable struct{}

func (noopRunnable) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (noopRunnable) Close() error                  { return nil }

// DefaultServiceName returns the OS service name of the agent (BeyzBackupAgent).
func DefaultServiceName() string { return DefaultName }

// ControlService performs an SCM/systemd action ("start", "stop", "restart",
// "install", "uninstall") on the named, already-installed service. It is intended
// for external controllers (the updater); the service must already be registered.
func ControlService(name, action string) error {
	svc, err := New(Config{Name: name, Runnable: noopRunnable{}})
	if err != nil {
		return err
	}
	return svc.Control(action)
}

// StatusService reports whether the named service is RUNNING. It fails CLOSED:
// any query error (not installed, SCM/systemd failure) yields (false, err), so a
// transient failure is never misread as running. Used by the updater's health gate.
func StatusService(name string) (bool, error) {
	svc, err := New(Config{Name: name, Runnable: noopRunnable{}})
	if err != nil {
		return false, err
	}
	st, err := svc.Status()
	if err != nil {
		return false, err
	}
	return st == StatusRunning, nil
}
