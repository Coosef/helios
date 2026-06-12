// Command mkfixtures writes the deterministic, signed update-manifest + artifact
// fixtures (internal/mocksaas) to test/fixtures/ for CI (T31) and integration (T34).
//
// It writes ONLY public material: the public keyset, the signed manifests, the
// artifacts, and an index of expected outcomes. The signing private key is derived
// from a fixed seed at runtime and is NEVER written (AC-35).
//
//	go run ./cmd/mkfixtures           # writes ./test/fixtures
//	go run ./cmd/mkfixtures -out DIR
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/beyzbackup/beyz-backup/internal/mocksaas"
)

func main() {
	out := flag.String("out", "test/fixtures", "output directory for the fixtures")
	flag.Parse()
	if err := mocksaas.WriteFixtures(*out); err != nil {
		fmt.Fprintln(os.Stderr, "mkfixtures:", err)
		os.Exit(1)
	}
	fmt.Printf("mkfixtures: wrote fixtures to %s\n", *out)
}
