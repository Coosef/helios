// Package trustpins exposes the COMPILED-IN SPKI bootstrap pin set for the agent
// control channel (ADR-005 §0.5). The bootstrap pin is the trust anchor baked
// into the signed binary — it is NOT read from operator-editable config (a pin in
// Users-readable config.yaml would be security-load-bearing and swappable).
//
// The value is injected at build time via ldflags, e.g.:
//
//	-ldflags "-X github.com/beyzbackup/beyz-backup/internal/agent/trustpins.bootstrapPins=sha256:<hex>[,sha256:<hex>]"
//
// It is EMPTY by default so a build with no pin FAILS CLOSED (the transport
// requires at least one pin; app.New returns ErrTransportInit on empty). The real
// production pin value is a Sprint-2 release-engineering input (the SaaS TLS leaf
// key does not exist until the server is built); Sprint-1 testing against the
// Prism mock injects the mock's pin via the same ldflags mechanism.
package trustpins

import "strings"

// bootstrapPins is the ldflags injection target: comma/space/newline-separated
// "sha256:<hex>" pins. Empty in an unconfigured build.
var bootstrapPins string

// Bootstrap returns the configured SPKI pins ("sha256:<hex>"), or nil when none
// are compiled in. nil propagates to app.Options.BootstrapPins, which fails closed.
func Bootstrap() []string {
	if strings.TrimSpace(bootstrapPins) == "" {
		return nil
	}
	fields := strings.FieldsFunc(bootstrapPins, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
