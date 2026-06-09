// Command beyz-backup-updater is the Beyz Backup on-demand updater.
//
// Per Technical Design §0.6 the updater is an ON-DEMAND binary (invoked by the
// agent or a scheduled task) and is NOT registered as a persistent Windows
// service. Sprint 1 implements REAL, enforcing Ed25519 signature, BLAKE3/SHA256
// hash, and anti-rollback verification (later Sprint 1 tasks); this entrypoint
// currently supports `--version` and a no-op run that reports the scaffold state.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/beyzbackup/beyz-backup/internal/buildinfo"
)

const binaryName = "beyz-backup-updater"

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
		"%s: Sprint 1 scaffold — updater logic is not yet implemented (on-demand binary, not a service). See docs/sprint-1/IMPLEMENTATION-PLAN.md.\n",
		info.String())
}
