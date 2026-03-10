package cli

import "strings"

// ShouldShowPerPURLDetails reports whether verbose per-PURL analysis blocks
// (displayBatchAnalysesFull) should be rendered to stdout.
//
// Suppression reasons (current):
//   - License CSV export mode: when LicenseCSVPath is non-empty, we intentionally
//     keep console noise low because the user is performing structured export.
//   - Discover static mode: discover-static has its own specialized output flow.
//
// Future extension (add conditions here instead of scattering if statements):
//   - Quiet / summary-only flags (e.g. --quiet, --summary-only)
//   - Fast license-only / performance modes
//   - Explicit --no-per-purl flag
func (o ProcessingOptions) ShouldShowPerPURLDetails() bool {
	// License export: suppress heavy stdout to keep output concise.
	if strings.TrimSpace(o.LicenseCSVPath) != "" {
		return false
	}
	return true
}
