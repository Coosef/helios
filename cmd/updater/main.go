// Command beyz-backup-updater is the Beyz Backup ON-DEMAND updater (Technical
// Design §0.6 / AC-42): it is invoked by a scheduled task or the agent, runs the
// update FSM to completion, and EXITS. It is NOT a persistent service, has NO
// self-update, and NO watchdog. Sprint 1 implements REAL Ed25519 signature +
// BLAKE3/SHA256 hash + anti-rollback verification (via T22–T26) behind a
// crash-recoverable FSM (internal/updater/app).
//
// Subcommands:
//
//	beyz-backup-updater --version    print version and exit
//	beyz-backup-updater check        fetch+verify+decide, print the decision, no side effects
//	beyz-backup-updater apply        run the full update FSM (stage→swap→health-gate→commit/rollback)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
	"github.com/beyzbackup/beyz-backup/internal/agent/config"
	"github.com/beyzbackup/beyz-backup/internal/agent/trustpins"
	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
	"github.com/beyzbackup/beyz-backup/internal/transport/httpclient"
	"github.com/beyzbackup/beyz-backup/internal/updater/app"
	"github.com/beyzbackup/beyz-backup/internal/updater/healthgate"
	"github.com/beyzbackup/beyz-backup/internal/updater/manifestcheck"
	"github.com/beyzbackup/beyz-backup/internal/updater/swap"
	"github.com/beyzbackup/beyz-backup/internal/updater/trust"
	"github.com/beyzbackup/beyz-backup/pkg/manifest"
)

const (
	binaryName       = "beyz-backup-updater"
	agentServiceName = "BeyzBackupAgent"
	agentBinaryName  = "beyz-backup-agent"
	defaultManifest  = "/v1/updates/manifest"
	maxManifestBytes = 1 << 20 // 1 MiB cap on the manifest body
)

// Exit codes: DISTINCT per terminal outcome so a scheduler/operator can react.
const (
	exitOK             = 0 // no update available, update succeeded, or clean recovery
	exitError          = 1 // config/state/generic failure
	exitRejected       = 2 // manifest rejected (signature/downgrade/stale/quarantine)
	exitRolledBack     = 3 // update applied but failed health gate; rolled back OK
	exitRollbackFailed = 4 // ROLLBACK RESTORE FAILED — manual recovery may be needed
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// runner is the orchestrator seam (satisfied by *app.Updater; faked in tests).
type runner interface {
	Check(ctx context.Context) (*manifestcheck.Decision, error)
	Apply(ctx context.Context) (app.Outcome, error)
}

// buildRunner constructs the production updater; overridable in tests.
var buildRunner = buildProdRunner

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(binaryName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var showVersion bool
	var configPath, stateDir, manifestURL, liveBinary, liveConfig, stagingDir string
	fs.BoolVar(&showVersion, "version", false, "print version information and exit")
	fs.StringVar(&configPath, "config", "", "path to config.yaml (default: OS-specific)")
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: OS-specific)")
	fs.StringVar(&manifestURL, "manifest-url", "", "override the manifest URL (default: <api_base_url>"+defaultManifest+")")
	fs.StringVar(&liveBinary, "live-binary", "", "override the live agent binary path")
	fs.StringVar(&liveConfig, "live-config", "", "override the live config path")
	fs.StringVar(&stagingDir, "staging-dir", "", "override the staging directory")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if showVersion {
		fmt.Fprintln(stdout, buildinfo.Get(binaryName).String())
		return exitOK
	}

	sub := fs.Arg(0)
	if sub != "check" && sub != "apply" {
		fmt.Fprintf(stderr, "%s: expected subcommand 'check' or 'apply' (or --version)\n", binaryName)
		return exitError
	}

	opts := bootstrapOptions{
		configPath:  orDefault(configPath, defaultConfigPath()),
		stateDir:    orDefault(stateDir, defaultStateDir()),
		manifestURL: manifestURL,
		liveBinary:  orDefault(liveBinary, defaultLiveBinary()),
		liveConfig:  orDefault(liveConfig, defaultConfigPath()),
		stagingDir:  orDefault(stagingDir, defaultStagingDir()),
	}
	r, err := buildRunner(opts)
	if err != nil {
		fmt.Fprintf(stderr, "%s: init failed: %v\n", binaryName, err)
		return exitError
	}
	return dispatch(context.Background(), sub, r, stdout, stderr)
}

// dispatch runs the subcommand and maps the outcome to an exit code.
func dispatch(ctx context.Context, sub string, r runner, stdout, stderr io.Writer) int {
	switch sub {
	case "check":
		dec, err := r.Check(ctx)
		if dec == nil {
			fmt.Fprintf(stderr, "%s: check failed: %v\n", binaryName, err)
			return exitError
		}
		if dec.Proceed {
			fmt.Fprintf(stdout, "update available: %s -> %s\n", dec.CurrentVersion, dec.TargetVersion)
			return exitOK
		}
		fmt.Fprintf(stdout, "no update: %s\n", dec.Reason)
		return exitOK // check never changes state; a rejection is informational
	case "apply":
		out, err := r.Apply(ctx)
		fmt.Fprintf(stdout, "apply: %s\n", out)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", binaryName, err)
		}
		return exitCodeFor(out)
	default:
		return exitError
	}
}

func exitCodeFor(out app.Outcome) int {
	switch out {
	case app.OutcomeUpdated, app.OutcomeNoUpdate, app.OutcomeRecovered:
		return exitOK
	case app.OutcomeRejected:
		return exitRejected
	case app.OutcomeRolledBack:
		return exitRolledBack
	case app.OutcomeRollbackFailed:
		return exitRollbackFailed
	default:
		return exitError
	}
}

// ---- production wiring --------------------------------------------------------

type bootstrapOptions struct {
	configPath, stateDir, manifestURL  string
	liveBinary, liveConfig, stagingDir string
}

func buildProdRunner(o bootstrapOptions) (runner, error) {
	cfg, err := config.Load(o.configPath, nil)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	pins := trustpins.Bootstrap()
	if len(pins) == 0 {
		return nil, errors.New("no SPKI bootstrap pins compiled in (fail closed)")
	}
	hc := httpclient.DefaultConfig()
	hc.Pins = pins
	hc.MaxResponseBytes = maxManifestBytes
	client, err := httpclient.New(hc)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}
	keys, err := trust.Embedded()
	if err != nil {
		return nil, fmt.Errorf("trust keyset: %w", err)
	}
	manifestURL := o.manifestURL
	if manifestURL == "" {
		manifestURL = cfg.General.APIBaseURL + defaultManifest
	}

	layout := swap.Layout{LiveBinary: o.liveBinary, LiveConfig: o.liveConfig, StagingDir: o.stagingDir}
	swapper, err := swap.New(layout, swap.NewHTTPDownloader(nil), 0)
	if err != nil {
		return nil, fmt.Errorf("swap: %w", err)
	}

	gate, err := healthgate.New(o.stateDir, app.ProdService{Name: agentServiceName}.Running)
	if err != nil {
		return nil, fmt.Errorf("healthgate: %w", err)
	}

	emitter, err := audit.New(audit.NewFileAppender(filepath.Join(o.stateDir, "updater-audit.jsonl")),
		io.Discard, audit.Identity{Source: audit.SourceUpdater, AgentVersion: buildinfo.Version})
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}

	buildVer, _ := manifest.ParseVersion(buildinfo.Version)

	return app.New(app.Deps{
		Store: app.NewStateStore(o.stateDir),
		Check: &app.ProdChecker{
			Client: client, URL: manifestURL, MaxBytes: maxManifestBytes,
			Keys: keys, Platform: runtime.GOOS, Arch: runtime.GOARCH,
		},
		Swap:         swapper,
		Gate:         gate,
		Service:      app.ProdService{Name: agentServiceName},
		Marker:       app.ProdMarker{StateDir: o.stateDir},
		Audit:        app.ProdAuditor{Emitter: emitter},
		BuildVersion: buildVer,
		TargetOS:     runtime.GOOS,
		Arch:         runtime.GOARCH,
	})
}

// ---- path defaults (install-determined; overridable via flags) ----------------

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func defaultBaseDir() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "BeyzBackup")
	}
	return "/var/lib/beyz-backup"
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(defaultBaseDir(), "config.yaml")
	}
	return "/etc/beyz-backup/config.yaml"
}

func defaultStateDir() string { return filepath.Join(defaultBaseDir(), "state") }

func defaultStagingDir() string { return filepath.Join(defaultBaseDir(), "update") }

func defaultLiveBinary() string {
	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		return filepath.Join(pf, "BeyzBackup", agentBinaryName+".exe")
	}
	return "/usr/local/bin/" + agentBinaryName
}
