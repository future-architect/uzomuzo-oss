package cli

import (
	"fmt"
	"sort"
	"strings"

	analysispkg "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// sortedAdvisoryBlock sorts advisories by CVSS3 descending, truncates to maxDisplayAdvisories,
// and formats each entry as an indented severity+ID line. indent is the leading whitespace per line.
func sortedAdvisoryBlock(advisories []analysispkg.Advisory, indent string) (lines []string, sorted []analysispkg.Advisory) {
	sorted = make([]analysispkg.Advisory, len(advisories))
	copy(sorted, advisories)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CVSS3Score > sorted[j].CVSS3Score
	})

	limit := len(sorted)
	if limit > maxDisplayAdvisories {
		limit = maxDisplayAdvisories
	}

	// severityCol is the fixed width for the severity column: "CRITICAL (9.8)" = 14 chars
	const severityColWidth = 14
	for _, adv := range sorted[:limit] {
		var sevCol string
		if adv.CVSS3Score > 0 && adv.Severity != "" {
			sevCol = fmt.Sprintf("%-8s (%.1f)", adv.Severity, adv.CVSS3Score)
		}
		sevCol = fmt.Sprintf("%-*s", severityColWidth, sevCol)
		lines = append(lines, fmt.Sprintf("%s%s  %s", indent, sevCol, adv.ID))
	}

	if len(sorted) > maxDisplayAdvisories {
		remaining := len(sorted) - maxDisplayAdvisories
		lines = append(lines, fmt.Sprintf("%s... and %d more", indent, remaining))
	}
	return lines, sorted
}

// formatAdvisoryLines formats advisory entries sorted by severity (highest first) with truncation.
// Shows up to maxDisplayAdvisories with ID and severity only (no title — detail is in linked page).
//
// Format with severity:  "  CRITICAL (9.8)  CVE-2024-9999"
// Format without:        "                  CVE-2024-1234"
func formatAdvisoryLines(advisories []analysispkg.Advisory) []string {
	if len(advisories) == 0 {
		return nil
	}

	lines, _ := sortedAdvisoryBlock(advisories, "  ")
	return lines
}

// advisoryCountText builds the advisory count annotation for a version line.
// Returns "" when no advisories exist or vd is nil.
func advisoryCountText(vd *analysispkg.VersionDetail) string {
	if vd == nil {
		return ""
	}
	direct := vd.DirectAdvisoryCount()
	transitive := vd.TransitiveAdvisoryCount()
	if direct == 0 && transitive == 0 {
		return ""
	}
	if direct == 0 {
		if transitive == 1 {
			return "  ⚠️ 1 transitive advisory"
		}
		return fmt.Sprintf("  ⚠️ %d transitive advisories", transitive)
	}
	base := "  ⚠️ 1 advisory"
	if direct > 1 {
		base = fmt.Sprintf("  ⚠️ %d advisories", direct)
	}
	if transitive == 0 {
		return base
	}
	return fmt.Sprintf("%s (+ %d transitive)", base, transitive)
}

// formatTransitiveAdvisoryLines formats transitive advisory entries grouped under a header.
// Shows dependency names in the header and advisory details indented beneath.
func formatTransitiveAdvisoryLines(advisories []analysispkg.Advisory) []string {
	if len(advisories) == 0 {
		return nil
	}

	advLines, sorted := sortedAdvisoryBlock(advisories, "    ")

	// Collect unique dependency names only from the displayed (truncated) subset
	// so the header stays consistent with the visible advisory lines.
	displayLimit := len(sorted)
	if displayLimit > maxDisplayAdvisories {
		displayLimit = maxDisplayAdvisories
	}
	seen := make(map[string]bool)
	var depNames []string
	for _, a := range sorted[:displayLimit] {
		if a.DependencyName != "" && !seen[a.DependencyName] {
			seen[a.DependencyName] = true
			depNames = append(depNames, a.DependencyName)
		}
	}

	// Build header: "Transitive (via dep1, dep2, dep3):" or with truncation
	const maxDepNames = 3
	header := "  Transitive"
	if len(depNames) > 0 {
		display := depNames
		suffix := ""
		if len(depNames) > maxDepNames {
			display = depNames[:maxDepNames]
			suffix = fmt.Sprintf(" and %d more", len(depNames)-maxDepNames)
		}
		header += fmt.Sprintf(" (via %s%s)", strings.Join(display, ", "), suffix)
	}
	header += ":"

	lines := make([]string, 0, 1+len(advLines))
	lines = append(lines, header)
	lines = append(lines, advLines...)
	return lines
}
