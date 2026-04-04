package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	commonlinks "github.com/future-architect/uzomuzo-oss/internal/common/links"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	analysispkg "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// defaultBarWidth is the character width of decorative ── bars in left-border output.
const defaultBarWidth = 60

// maxDisplayAdvisories is the maximum number of advisories shown inline.
// Remaining advisories are summarized with a deps.dev link.
const maxDisplayAdvisories = 3

// minBarPadding is the minimum number of trailing ─ characters on a bar line.
const minBarPadding = 3

// boxContext holds all data needed to render a single box entry.
type boxContext struct {
	w        io.Writer
	entry    *domainaudit.AuditEntry
	analysis *analysispkg.Analysis // shortcut: entry.Analysis (may be nil)
	barWidth int
}

// newBoxContext creates a boxContext from an AuditEntry.
func newBoxContext(w io.Writer, entry *domainaudit.AuditEntry, barWidth int) *boxContext {
	return &boxContext{
		w:        w,
		entry:    entry,
		analysis: entry.Analysis,
		barWidth: barWidth,
	}
}

// ---------------------------------------------------------------------------
// Border primitives (left-border only — no right border)
// ---------------------------------------------------------------------------

// writeTopBar writes: ── title ──────────...
func writeTopBar(ctx *boxContext) error {
	title := boxTitle(ctx.entry)
	bar := buildBar("──", " "+title+" ", ctx.barWidth)
	if _, err := fmt.Fprintln(ctx.w, bar); err != nil {
		return fmt.Errorf("failed to write top bar: %w", err)
	}
	return nil
}

// writeSectionBar writes: ├─ label ──────────...
func writeSectionBar(ctx *boxContext, label string) error {
	bar := buildBar("├─", " "+label+" ", ctx.barWidth)
	if _, err := fmt.Fprintln(ctx.w, bar); err != nil {
		return fmt.Errorf("failed to write section bar %q: %w", label, err)
	}
	return nil
}

// writeBottomBar writes: └──────────────────...
func writeBottomBar(ctx *boxContext) error {
	if _, err := fmt.Fprintln(ctx.w, "└"+strings.Repeat("─", ctx.barWidth-1)); err != nil {
		return fmt.Errorf("failed to write bottom bar: %w", err)
	}
	return nil
}

// writeLine writes: │ content
func writeLine(ctx *boxContext, format string, args ...any) error {
	content := fmt.Sprintf(format, args...)
	if _, err := fmt.Fprintf(ctx.w, "│ %s\n", content); err != nil {
		return fmt.Errorf("failed to write box line: %w", err)
	}
	return nil
}

// buildBar constructs a decorative bar like "── title ────────..." or "├─ label ────────...".
// Uses rune count (not byte count) so multi-byte box-drawing characters size correctly.
func buildBar(prefix, middle string, width int) string {
	remaining := width - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(middle)
	if remaining < 0 {
		remaining = 0
	} else if remaining < minBarPadding {
		remaining = minBarPadding
	}
	return prefix + middle + strings.Repeat("─", remaining)
}

// ---------------------------------------------------------------------------
// Title & verdict helpers
// ---------------------------------------------------------------------------

// boxTitle returns the PURL with optional source/relation annotation for the top bar.
func boxTitle(e *domainaudit.AuditEntry) string {
	purl := e.PURL
	if e.Source != domainaudit.SourceDirect {
		return fmt.Sprintf("[%s] %s", sourceDisplayName(e.Source), purl)
	}
	if e.Relation == depparser.RelationTransitive {
		return fmt.Sprintf("%s (transitive)", purl)
	}
	return purl
}

// verdictIcon returns the emoji for a given verdict.
func verdictIcon(v domainaudit.Verdict) string {
	switch v {
	case domainaudit.VerdictOK:
		return "✅"
	case domainaudit.VerdictCaution:
		return "⚠️"
	case domainaudit.VerdictReplace:
		return "🔴"
	default:
		return "🔍"
	}
}

// verdictLabel returns the human-readable label for a verdict.
func verdictLabel(v domainaudit.Verdict) string {
	switch v {
	case domainaudit.VerdictOK:
		return "OK"
	case domainaudit.VerdictCaution:
		return "Caution"
	case domainaudit.VerdictReplace:
		return "Replace"
	default:
		return "Review Needed"
	}
}

// ---------------------------------------------------------------------------
// Section renderers
// ---------------------------------------------------------------------------

// writeBoxIdentity writes the Identity section (package, description).
// Homepage and Registry URLs are rendered in the Links section instead.
func writeBoxIdentity(ctx *boxContext) error {
	a := ctx.analysis
	// Skip Package: line when it would be identical to the top bar title
	displayPackage := ctx.entry.PURL
	if a != nil {
		if dp := a.DisplayPURL(); dp != "" && dp != ctx.entry.PURL {
			displayPackage = dp
		}
	}
	if displayPackage != boxTitle(ctx.entry) {
		if err := writeLine(ctx, "Package: %s", displayPackage); err != nil {
			return err
		}
	}
	if a != nil && a.Repository != nil && a.Repository.Description != "" {
		if desc := truncateDescription(a.Repository.Description); desc != "" {
			if err := writeLine(ctx, "Description: %s", desc); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeBoxOrigin writes the Origin section (source, relation, via).
// Returns nil without writing for direct PURLs with direct/unknown relation (no provenance noise).
// Only shown for action/transitive entries where origin context is meaningful.
func writeBoxOrigin(ctx *boxContext) error {
	hasOrigin := ctx.entry.Source != domainaudit.SourceDirect ||
		ctx.entry.Relation == depparser.RelationTransitive ||
		ctx.entry.Via != ""
	if !hasOrigin {
		return nil
	}
	if err := writeSectionBar(ctx, "Origin"); err != nil {
		return err
	}
	if ctx.entry.Source != domainaudit.SourceDirect {
		if err := writeLine(ctx, "Source: %s", sourceDisplayName(ctx.entry.Source)); err != nil {
			return err
		}
	}
	if ctx.entry.Relation == depparser.RelationTransitive {
		if err := writeLine(ctx, "Relation: %s", formatRelation(ctx.entry)); err != nil {
			return err
		}
	}
	if ctx.entry.Via != "" {
		if err := writeLine(ctx, "Via: %s", ctx.entry.Via); err != nil {
			return err
		}
	}
	return nil
}

// writeBoxVerdict writes the Status section with emoji icon.
func writeBoxVerdict(ctx *boxContext) error {
	if err := writeSectionBar(ctx, "Status"); err != nil {
		return err
	}
	icon := verdictIcon(ctx.entry.Verdict)
	label := verdictLabel(ctx.entry.Verdict)

	// Use lifecycle label if available for more specific display
	if ctx.analysis != nil {
		if lr := ctx.analysis.GetLifecycleResult(); lr != nil {
			label = string(lr.Label)
		}
	}

	if err := writeLine(ctx, "%s %s", icon, label); err != nil {
		return err
	}
	if ctx.analysis != nil {
		if lr := ctx.analysis.GetLifecycleResult(); lr != nil && lr.Reason != "" {
			if err := writeLine(ctx, "Reason: %s", lr.Reason); err != nil {
				return err
			}
		}
	}
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		if ctx.analysis != nil {
			if lr := ctx.analysis.GetLifecycleResult(); lr != nil && len(lr.Trace) > 0 {
				for i, step := range lr.Trace {
					if err := writeLine(ctx, "  Trace[%d]: %s", i, step); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// writeBoxEOL writes the EOL section (evidence, catalog, successor).
// Returns nil without writing if no EOL data exists.
func writeBoxEOL(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}
	hasEOL := len(a.EOL.Evidences) > 0 ||
		(a.EOL.ScheduledAt != nil && a.EOL.State == analysispkg.EOLScheduled) ||
		a.EOL.Successor != "" ||
		a.EOL.Reason != ""
	if !hasEOL {
		return nil
	}
	if err := writeSectionBar(ctx, "EOL"); err != nil {
		return err
	}
	if a.EOL.ScheduledAt != nil && a.EOL.State == analysispkg.EOLScheduled {
		if err := writeLine(ctx, "⚠️ Scheduled EOL: %s", a.EOL.ScheduledAt.Format(dateFormat)); err != nil {
			return err
		}
	}
	if a.EOL.Successor != "" {
		if err := writeLine(ctx, "➡️ Successor: %s", a.EOL.Successor); err != nil {
			return err
		}
	}
	if a.EOL.Reason != "" {
		if err := writeLine(ctx, "Catalog Reason: %s", a.EOL.Reason); err != nil {
			return err
		}
	}
	if len(a.EOL.Evidences) > 0 {
		if err := writeLine(ctx, "Evidence (%d):", len(a.EOL.Evidences)); err != nil {
			return err
		}
		for _, ev := range a.EOL.Evidences {
			line := ""
			if ev.Source != "" {
				line = fmt.Sprintf("[%s] %s", ev.Source, ev.Summary)
			} else {
				line = ev.Summary
			}
			if ev.Confidence > 0 {
				line += fmt.Sprintf(" (confidence %.2f)", ev.Confidence)
			}
			if err := writeLine(ctx, "  %s", line); err != nil {
				return err
			}
			if ref := strings.TrimSpace(ev.Reference); ref != "" {
				if err := writeLine(ctx, "    ↳ %s", ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// writeBoxHealth writes the Health section (repo state, dependents, scores, commit activity).
// Returns nil without writing if no health data exists.
func writeBoxHealth(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}

	var lines []string

	// Repo state — only show anomalous states (Archived/Disabled/Fork).
	// "Normal" is omitted as it carries no information.
	if a.RepoState != nil {
		if a.RepoState.IsArchived {
			lines = append(lines, "📦 Archived")
		} else if a.RepoState.IsDisabled {
			lines = append(lines, "⛔ Disabled")
		} else if a.RepoState.IsFork {
			if a.RepoState.ForkSource != "" {
				lines = append(lines, fmt.Sprintf("⚠️ Fork of %s", a.RepoState.ForkSource))
			} else {
				lines = append(lines, "⚠️ Fork")
			}
		}
	}
	if a.Repository != nil && a.Repository.StarsCount > 0 {
		lines = append(lines, fmt.Sprintf("%d stars", a.Repository.StarsCount))
	}

	// Dependent count
	if a.DependentCount > 0 {
		lines = append(lines, fmt.Sprintf("Used by: %d packages", a.DependentCount))
	}
	if a.DirectDepsCount > 0 || a.TransitiveDepsCount > 0 {
		lines = append(lines, fmt.Sprintf("Depends on: %d direct, %d transitive", a.DirectDepsCount, a.TransitiveDepsCount))
	}

	// Scores
	if len(a.Scores) > 0 {
		scoreLine := fmt.Sprintf("Score: %.*f/10", scorePrecision, a.OverallScore)

		// Sort score names for deterministic output
		var scoreNames []string
		for name := range a.Scores {
			scoreNames = append(scoreNames, name)
		}
		sort.Strings(scoreNames)

		for _, name := range scoreNames {
			scoreEntity := a.Scores[name]
			if scoreEntity == nil {
				slog.Debug("Skipping nil score entity", "check", name)
				continue
			}
			if name == "Maintained" && scoreEntity.Value() >= 0 {
				scoreLine += fmt.Sprintf("  Maintained: %.*f/10", scorePrecision, float64(scoreEntity.Value()))
			}
			if name == "Vulnerabilities" && scoreEntity.Value() >= 0 {
				scoreLine += fmt.Sprintf("  Vuln: %.*f/10", scorePrecision, float64(scoreEntity.Value()))
			}
		}
		lines = append(lines, scoreLine)
	}

	// Commit activity
	if a.RepoState != nil && a.RepoState.LatestHumanCommit != nil && !a.RepoState.LatestHumanCommit.IsZero() {
		lines = append(lines, fmt.Sprintf("Last Commit: %s", a.RepoState.LatestHumanCommit.Format(dateFormat)))
	}

	// Only write section if we have meaningful data beyond the hint
	if len(lines) == 0 {
		return nil
	}

	if err := writeSectionBar(ctx, "Health"); err != nil {
		return err
	}
	for _, line := range lines {
		if err := writeLine(ctx, "%s", line); err != nil {
			return err
		}
	}
	return nil
}

// writeBoxReleases writes the Releases section (stable, pre-release, max semver, requested version).
// Returns nil without writing if no release data exists.
func writeBoxReleases(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil || a.ReleaseInfo == nil {
		return nil
	}

	var lines []string
	eco, name := packageEcoName(a)

	stableVer := ""

	// Stable version — gate on Version, not PublishedAt, so advisories are never hidden.
	if a.ReleaseInfo.StableVersion != nil && a.ReleaseInfo.StableVersion.Version != "" {
		stable := a.ReleaseInfo.StableVersion
		stableVer = stable.Version
		deprecated := ""
		if stable.IsDeprecated {
			deprecated = " ⚠️ [DEPRECATED]"
		}
		advCount := len(stable.Advisories)
		advText := ""
		if advCount > 0 {
			advText = fmt.Sprintf("  ⚠️ Advisories: %d%s", advCount, advisorySeveritySummary(stable))
		}
		if !stable.PublishedAt.IsZero() {
			lines = append(lines, fmt.Sprintf("Stable: %s (%s)%s%s",
				stable.Version, stable.PublishedAt.Format(dateFormat), advText, deprecated))
		} else {
			lines = append(lines, fmt.Sprintf("Stable: %s%s%s",
				stable.Version, advText, deprecated))
		}
		lines = append(lines, formatAdvisoryLines(stable.Advisories, eco, name, stable.Version)...)
	}

	preVer := ""

	// Pre-release (skip if same version as stable)
	if a.ReleaseInfo.PreReleaseVersion != nil && a.ReleaseInfo.PreReleaseVersion.Version != "" {
		pre := a.ReleaseInfo.PreReleaseVersion
		// Always track preVer for downstream dedup even when skipped
		preVer = pre.Version
		if pre.Version != stableVer {
			deprecated := ""
			if pre.IsDeprecated {
				deprecated = " ⚠️ [DEPRECATED]"
			}
			if !pre.PublishedAt.IsZero() {
				lines = append(lines, fmt.Sprintf("Pre-release: %s (%s)%s",
					pre.Version, pre.PublishedAt.Format(dateFormat), deprecated))
			} else {
				lines = append(lines, fmt.Sprintf("Pre-release: %s%s",
					pre.Version, deprecated))
			}
		}
	}

	// Max semver (skip if same as pre-release or stable)
	if a.ReleaseInfo.MaxSemverVersion != nil && a.ReleaseInfo.MaxSemverVersion.Version != "" {
		maxv := a.ReleaseInfo.MaxSemverVersion
		if maxv.Version != stableVer && maxv.Version != preVer {
			deprecated := ""
			if maxv.IsDeprecated {
				deprecated = " ⚠️ [DEPRECATED]"
			}
			if !maxv.PublishedAt.IsZero() {
				lines = append(lines, fmt.Sprintf("Highest (SemVer): %s (%s)%s",
					maxv.Version, maxv.PublishedAt.Format(dateFormat), deprecated))
			} else {
				lines = append(lines, fmt.Sprintf("Highest (SemVer): %s%s", maxv.Version, deprecated))
			}
		}
	}

	// Requested version (skip if same as stable or pre-release)
	if a.ReleaseInfo.RequestedVersion != nil && a.ReleaseInfo.RequestedVersion.Version != "" {
		rv := a.ReleaseInfo.RequestedVersion
		if rv.Version != stableVer && rv.Version != preVer {
			deprecated := ""
			if rv.IsDeprecated {
				deprecated = " ⚠️ [DEPRECATED]"
			}
			if !rv.PublishedAt.IsZero() {
				lines = append(lines, fmt.Sprintf("Requested: %s (%s)%s",
					rv.Version, rv.PublishedAt.Format(dateFormat), deprecated))
			} else {
				lines = append(lines, fmt.Sprintf("Requested: %s%s",
					rv.Version, deprecated))
			}
		}
	}

	if len(lines) == 0 {
		return nil
	}

	if err := writeSectionBar(ctx, "Releases"); err != nil {
		return err
	}
	for _, line := range lines {
		if err := writeLine(ctx, "%s", line); err != nil {
			return err
		}
	}
	return nil
}

// formatAdvisoryLines formats advisory entries sorted by severity (highest first) with truncation.
// Shows up to maxDisplayAdvisories with aligned columns, then a deps.dev link for the full list.
//
// Format with severity:  "  CRITICAL (9.8)  CVE-2024-9999  Crash in HeaderParser"
// Format without:        "                  CVE-2024-1234"
func formatAdvisoryLines(advisories []analysispkg.Advisory, ecosystem, name, version string) []string {
	if len(advisories) == 0 {
		return nil
	}

	// Sort by CVSS3 descending (unknown/0 at end), stable sort preserves order for equal scores
	sorted := make([]analysispkg.Advisory, len(advisories))
	copy(sorted, advisories)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CVSS3Score > sorted[j].CVSS3Score
	})

	var lines []string
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
		// Pad severity column to fixed width for alignment
		sevCol = fmt.Sprintf("%-*s", severityColWidth, sevCol)

		title := ""
		if adv.Title != "" {
			title = "  " + adv.Title
		}
		lines = append(lines, fmt.Sprintf("  %s  %s%s", sevCol, adv.ID, title))

		advisoryURL := strings.TrimSpace(adv.URL)
		if advisoryURL != "" {
			lines = append(lines, fmt.Sprintf("  → %s", advisoryURL))
		}
	}

	if len(sorted) > maxDisplayAdvisories {
		remaining := len(sorted) - maxDisplayAdvisories
		lines = append(lines, fmt.Sprintf("  ... and %d more", remaining))
	}

	// Always show deps.dev link when advisories exist
	depsdevURL := commonlinks.BuildDepsDevVersionURL(ecosystem, name, version)
	if depsdevURL != "" {
		lines = append(lines, fmt.Sprintf("  → %s", depsdevURL))
	}

	return lines
}

// advisorySeveritySummary returns a severity summary string for the advisory count line.
// e.g., " (max: CRITICAL 9.8)" or " (max: HIGH 7.5, 2 unknown)" or "".
func advisorySeveritySummary(vd *analysispkg.VersionDetail) string {
	if vd == nil || len(vd.Advisories) == 0 {
		return ""
	}
	unknownCount := vd.UnknownSeverityAdvisoryCount()
	maxScore := vd.MaxCVSS3()

	if maxScore <= 0 {
		if unknownCount > 0 {
			return fmt.Sprintf(" (%d unknown)", unknownCount)
		}
		return ""
	}

	severity := analysispkg.SeverityFromCVSS3(maxScore)
	if unknownCount > 0 {
		return fmt.Sprintf(" (max: %s %.1f, %d unknown)", severity, maxScore, unknownCount)
	}
	return fmt.Sprintf(" (max: %s %.1f)", severity, maxScore)
}

// writeBoxLicenses writes the License section.
// Returns nil without writing if no license data exists.
func writeBoxLicenses(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}
	proj := a.ProjectLicense
	reqs := a.RequestedVersionLicenses
	if proj.IsZero() && len(reqs) == 0 {
		return nil
	}

	if err := writeSectionBar(ctx, "License"); err != nil {
		return err
	}

	// Collapse when project and single version license match
	collapse := proj.Identifier != "" && len(reqs) == 1 && strings.EqualFold(proj.Identifier, reqs[0].Identifier)
	if collapse {
		projShort := shortenLicenseSource(proj.Source)
		verShort := shortenLicenseSource(reqs[0].Source)
		switch {
		case proj.Source != "" && reqs[0].Source != "":
			if projShort == verShort {
				return writeLine(ctx, "%s (%s)", proj.Identifier, projShort)
			}
			return writeLine(ctx, "%s (project: %s / version: %s)", proj.Identifier, projShort, verShort)
		case proj.Source != "":
			return writeLine(ctx, "%s (%s)", proj.Identifier, projShort)
		case reqs[0].Source != "":
			return writeLine(ctx, "%s (%s)", proj.Identifier, verShort)
		default:
			return writeLine(ctx, "%s", proj.Identifier)
		}
	}

	// Project license
	if proj.Identifier != "" {
		if proj.Source != "" {
			if err := writeLine(ctx, "Project: %s (%s)", proj.Identifier, shortenLicenseSource(proj.Source)); err != nil {
				return err
			}
		} else {
			if err := writeLine(ctx, "Project: %s", proj.Identifier); err != nil {
				return err
			}
		}
	} else if proj.IsNonStandard() && proj.Raw != "" {
		if err := writeLine(ctx, "Project: (non-standard raw=%s)", proj.Raw); err != nil {
			return err
		}
	} else if proj.IsZero() {
		if err := writeLine(ctx, "Project: (not detected)"); err != nil {
			return err
		}
	} else {
		if err := writeLine(ctx, "Project: (unclassified raw=%s)", proj.Raw); err != nil {
			return err
		}
	}

	// Version licenses
	if len(reqs) > 0 {
		allSameSource := true
		firstSource := reqs[0].Source
		for _, rl := range reqs {
			if rl.Source != firstSource {
				allSameSource = false
				break
			}
		}
		if allSameSource {
			ids := make([]string, 0, len(reqs))
			for _, rl := range reqs {
				ids = append(ids, rl.Identifier)
			}
			if firstSource != "" {
				if err := writeLine(ctx, "Requested Version: %s (%s)", strings.Join(ids, ", "), shortenLicenseSource(firstSource)); err != nil {
					return err
				}
			} else {
				if err := writeLine(ctx, "Requested Version: %s", strings.Join(ids, ", ")); err != nil {
					return err
				}
			}
		} else {
			for i, rl := range reqs {
				if rl.Source != "" {
					if err := writeLine(ctx, "Requested Version[%d]: %s (%s)", i, rl.Identifier, shortenLicenseSource(rl.Source)); err != nil {
						return err
					}
				} else {
					if err := writeLine(ctx, "Requested Version[%d]: %s", i, rl.Identifier); err != nil {
						return err
					}
				}
			}
		}
	} else {
		if err := writeLine(ctx, "Requested Version: (none)"); err != nil {
			return err
		}
	}
	return nil
}

// writeBoxLinks writes the Links section (homepage, repository, registry, deps.dev).
// Returns nil without writing if no URLs exist.
func writeBoxLinks(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}

	var lines []string

	// Homepage and Registry moved here from Identity section
	if a.PackageLinks != nil {
		if a.PackageLinks.HomepageURL != "" {
			lines = append(lines, fmt.Sprintf("Homepage: %s", a.PackageLinks.HomepageURL))
		}
	}
	if a.RepoURL != "" {
		repoURL := a.RepoURL
		lower := strings.ToLower(repoURL)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			repoURL = "https://" + repoURL
		}
		lines = append(lines, fmt.Sprintf("Repository: %s", repoURL))
	}
	if a.PackageLinks != nil {
		if a.PackageLinks.RegistryURL != "" {
			lines = append(lines, fmt.Sprintf("Registry: %s", a.PackageLinks.RegistryURL))
		}
	}

	// deps.dev link (package-level, no version)
	eco, name := packageEcoName(a)
	if depsdevURL := commonlinks.BuildDepsDevURL(eco, name); depsdevURL != "" {
		lines = append(lines, fmt.Sprintf("deps.dev: %s", depsdevURL))
	}

	if len(lines) == 0 {
		return nil
	}

	if err := writeSectionBar(ctx, "Links"); err != nil {
		return err
	}
	for _, line := range lines {
		if err := writeLine(ctx, "%s", line); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Orchestrators
// ---------------------------------------------------------------------------

// renderBoxEntry writes a complete left-border box for one AuditEntry.
func renderBoxEntry(w io.Writer, entry *domainaudit.AuditEntry) error {
	ctx := newBoxContext(w, entry, defaultBarWidth)

	if entry.Analysis == nil || entry.Analysis.Error != nil {
		return renderBoxEntryError(ctx)
	}

	for _, fn := range []func() error{
		func() error { return writeTopBar(ctx) },
		func() error { return writeBoxIdentity(ctx) },
		func() error { return writeBoxOrigin(ctx) },
		func() error { return writeBoxVerdict(ctx) },
		func() error { return writeBoxEOL(ctx) },
		func() error { return writeBoxHealth(ctx) },
		func() error { return writeBoxReleases(ctx) },
		func() error { return writeBoxLicenses(ctx) },
		func() error { return writeBoxLinks(ctx) },
		func() error { return writeBottomBar(ctx) },
	} {
		if err := fn(); err != nil {
			return fmt.Errorf("failed to render box for %s: %w", entry.PURL, err)
		}
	}
	return nil
}

// renderBoxEntryError writes a minimal box for entries with nil analysis or errors.
func renderBoxEntryError(ctx *boxContext) error {
	wrap := func(err error) error {
		return fmt.Errorf("failed to render error box for %s: %w", ctx.entry.PURL, err)
	}
	if err := writeTopBar(ctx); err != nil {
		return wrap(err)
	}
	// Skip Package: line when identical to top bar title (consistent with writeBoxIdentity)
	if ctx.entry.PURL != boxTitle(ctx.entry) {
		if err := writeLine(ctx, "Package: %s", ctx.entry.PURL); err != nil {
			return wrap(err)
		}
	}
	if ctx.entry.Via != "" {
		if err := writeLine(ctx, "Via: %s", ctx.entry.Via); err != nil {
			return wrap(err)
		}
	}
	if err := writeSectionBar(ctx, "Status"); err != nil {
		return wrap(err)
	}
	icon := verdictIcon(ctx.entry.Verdict)
	label := verdictLabel(ctx.entry.Verdict)
	if ctx.entry.ErrorMsg != "" {
		if err := writeLine(ctx, "%s %s (error: %s)", icon, label, ctx.entry.ErrorMsg); err != nil {
			return wrap(err)
		}
	} else {
		if err := writeLine(ctx, "%s %s", icon, label); err != nil {
			return wrap(err)
		}
	}
	if err := writeBottomBar(ctx); err != nil {
		return wrap(err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// shortenLicenseSource abbreviates verbose license source identifiers for display.
// e.g. "depsdev-project-spdx" → "depsdev", "project-fallback" → "fallback".
func shortenLicenseSource(s string) string {
	switch s {
	case analysispkg.LicenseSourceDepsDevProjectSPDX,
		analysispkg.LicenseSourceDepsDevProjectNonStandard,
		analysispkg.LicenseSourceDepsDevVersionSPDX,
		analysispkg.LicenseSourceDepsDevVersionRaw:
		return "depsdev"
	case analysispkg.LicenseSourceGitHubProjectSPDX,
		analysispkg.LicenseSourceGitHubProjectNonStandard,
		analysispkg.LicenseSourceGitHubVersionSPDX,
		analysispkg.LicenseSourceGitHubVersionRaw:
		return "github"
	case analysispkg.LicenseSourceProjectFallback:
		return "fallback"
	case analysispkg.LicenseSourceDerivedFromVersion:
		return "derived"
	default:
		return s
	}
}

// packageEcoName extracts ecosystem and package name suitable for deps.dev URLs.
// It uses Namespace()+Name() (not GetPackageName()) so that scoped npm packages,
// composer vendor/name, and golang module paths are preserved without URL-escaping.
// Uses the EffectivePURL (resolved PURL) to parse ecosystem and API-compatible name.
func packageEcoName(a *analysispkg.Analysis) (ecosystem, name string) {
	if a == nil {
		return "", ""
	}
	raw := a.EffectivePURL
	if raw == "" {
		raw = a.OriginalPURL
	}
	if raw == "" {
		return "", ""
	}
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(raw)
	if err != nil {
		return "", ""
	}
	ns := parsed.Namespace()
	name = parsed.Name()
	if ns != "" {
		name = ns + "/" + name
	}
	return parsed.GetEcosystem(), name
}
