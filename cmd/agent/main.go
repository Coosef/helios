// Command beyz-backup-agent is the Beyz Backup Windows/Linux backup agent.
//
// Sprint 1 ships the agent FOUNDATION only. This entrypoint is the composition
// root that later Sprint 1 tasks wire up (configuration, structured logging,
// OS service lifecycle, enrollment, heartbeat, and task polling). It currently
// supports `--version` and a no-op run that reports the scaffold state; no
// backup, restore, compression, or encryption behaviour exists yet (those are
// Sprints 3-6).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
)

const binaryName = "beyz-backup-agent"

func main() {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.Parse()

	info := buildinfo.Get(binaryName)
	if showVersion {
		fmt.Println(info.String())
		return
	}

	fmt.Fprintf(os.Stdout,
		"%s: Sprint 1 scaffold — the agent runtime is not yet implemented. See docs/sprint-1/IMPLEMENTATION-PLAN.md.\n",
		info.String())
}
