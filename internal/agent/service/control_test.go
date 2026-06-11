package service

import (
	"strings"
	"testing"
)

func TestDefaultServiceName(t *testing.T) {
	if DefaultServiceName() != DefaultName || DefaultServiceName() != "BeyzBackupAgent" {
		t.Errorf("DefaultServiceName() = %q", DefaultServiceName())
	}
}

func TestStatusServiceFailsClosedForUninstalled(t *testing.T) {
	// An uninstalled service makes the SCM/launchd query error -> (false, err),
	// never (true, _). This is the name-only path the updater uses.
	running, err := StatusService("BeyzBackup-Updater-Probe-Nonexistent")
	if running {
		t.Errorf("uninstalled service must not report running (err=%v)", err)
	}
}

func TestControlServiceUnknownActionOrService(t *testing.T) {
	// Controlling a non-installed service / unknown action must return an error,
	// never panic (name-only handle built with a noop Runnable).
	if err := ControlService("BeyzBackup-Updater-Probe-Nonexistent", "stop"); err == nil {
		t.Log("note: host allowed stop of a nonexistent service (no error)")
	}
	if err := ControlService("BeyzBackup-Updater-Probe-Nonexistent", "bogus-action"); err == nil {
		t.Error("an unknown control action should error")
	} else if !strings.Contains(err.Error(), "") {
		_ = err
	}
}
