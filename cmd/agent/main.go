// Command beyz-backup-agent is the Windows/Linux endpoint agent.
//
// It runs the agent composition root (internal/agent/app) under the OS service
// lifecycle adapter (internal/agent/service): as a Windows Service (SCM), a Linux
// systemd service, or a foreground console process. The SPKI bootstrap pin is
// compiled in (internal/agent/trustpins); a build with no pin fails closed.
// Sprint-1 scope: no backup/restore engine, no installer, no updater service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/beyzbackup/beyz-backup/internal/agent/app"
	"github.com/beyzbackup/beyz-backup/internal/agent/paths"
	"github.com/beyzbackup/beyz-backup/internal/agent/service"
	"github.com/beyzbackup/beyz-backup/internal/agent/trustpins"
	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
)

const (
	binaryName         = "beyz-backup-agent"
	serviceName        = "BeyzBackupAgent" // frozen identifier; Phase-3 rename deferred
	serviceDescription = "Endpoint agent service (backup/restore foundation)."
)

// Exit codes. Terminal credential/version outcomes get DISTINCT codes so an
// operator / systemd can distinguish them and NOT spin a restart loop.
const (
	exitOK             = 0
	exitError          = 1  // startup/config/state/transport or generic failure
	exitReEnroll       = 10 // 401 -> re-enrollment required (do not auto-restart)
	exitUpgrade        = 11 // 426 -> protocol upgrade required (route to updater)
	exitAlreadyRunning = 12 // single-instance lock held by another process
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run parses flags and dispatches. It is the testable entrypoint.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(binaryName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var showVersion, foreground bool
	var configPath string
	fs.BoolVar(&showVersion, "version", false, "print version information and exit")
	fs.BoolVar(&foreground, "foreground", false, "run in the foreground (console) instead of as a service")
	fs.StringVar(&configPath, "config", "", "path to config.yaml (default: OS-specific)")
	if err := fs.Parse(args); err != nil {
		return exitError
	}

	if showVersion {
		fmt.Fprintln(stdout, buildinfo.Get(binaryName).String())
		return exitOK
	}

	// Optional dev control verbs: install | uninstall | start | stop | restart.
	if rest := fs.Args(); len(rest) > 0 {
		return control(rest[0], configPath, stdout, stderr)
	}
	return serve(configPath, foreground, stderr)
}

// serve builds the app + service and runs it, mapping the terminal outcome to an
// exit code. Startup failures print only the typed error (never config/secrets).
func serve(configPath string, foreground bool, stderr io.Writer) int {
	p := paths.Default()
	if configPath == "" {
		configPath = p.ConfigPath
	}
	a, err := app.New(app.Options{
		ConfigPath:    configPath,
		StateDir:      p.StateDir,
		BootstrapPins: trustpins.Bootstrap(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: startup failed: %v\n", binaryName, err)
		return exitCodeFor(err)
	}
	svc, serr := service.New(service.Config{
		Name:        serviceName,
		DisplayName: serviceName,
		Description: serviceDescription,
		Runnable:    a,
		Log:         a.Logger(),
		LockPath:    p.LockPath,
		Arguments:   []string{"--config", configPath},
		Foreground:  foreground,  // forced foreground under the systemd unit (--foreground)
		ExitFunc:    exitCodeFor, // watchdog last-resort exit uses the mapped code
	})
	if serr != nil {
		_ = a.Close()
		fmt.Fprintf(stderr, "%s: service init: %v\n", binaryName, serr)
		return exitError
	}
	return exitCodeFor(svc.Run())
}

// control runs an OS service-manager action (dev/Linux convenience; the production
// Windows install is installer-driven, T29). It uses a no-op runnable since the
// workload is never started for a control action.
func control(verb, configPath string, stdout, stderr io.Writer) int {
	switch verb {
	case "install", "uninstall", "start", "stop", "restart":
	default:
		fmt.Fprintf(stderr, "%s: unknown command %q (want: install|uninstall|start|stop|restart)\n", binaryName, verb)
		return exitError
	}
	if configPath == "" {
		configPath = paths.Default().ConfigPath
	}
	svc, err := service.New(service.Config{
		Name:        serviceName,
		DisplayName: serviceName,
		Description: serviceDescription,
		Runnable:    noopRunnable{},
		Arguments:   []string{"--config", configPath},
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", binaryName, err)
		return exitError
	}
	if err := svc.Control(verb); err != nil {
		fmt.Fprintf(stderr, "%s: %s failed: %v\n", binaryName, verb, err)
		return exitError
	}
	fmt.Fprintf(stdout, "%s: %s ok\n", binaryName, verb)
	return exitOK
}

// exitCodeFor maps a terminal app/service outcome to a process exit code.
func exitCodeFor(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, app.ErrEnrollmentRequired):
		return exitReEnroll
	case errors.Is(err, app.ErrUpgradeRequired):
		return exitUpgrade
	case errors.Is(err, service.ErrAlreadyRunning):
		return exitAlreadyRunning
	default:
		return exitError
	}
}

// noopRunnable supervises nothing; used only for control actions (install/etc.).
type noopRunnable struct{}

func (noopRunnable) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (noopRunnable) Close() error                  { return nil }
