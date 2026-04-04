package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	analysispkg "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"

	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// defaultBarWidth is the character width of decorative ── bars in left-border output.
const defaultBarWidth = 60

// maxDisplayAdvisories is the maximum number of advisories shown inline.
// Remaining advisories are summarized with a deps.dev link.
const maxDisplayAdvisories = 3

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
	_, err := fmt.Fprintln(ctx.w, bar)
	return err
}

// writeSectionBar writes: ├─ label ──────────...
func writeSectionBar(ctx *boxContext, label string) error {
	bar := buildBar("├─", " "+label+" ", ctx.barWidth)
	_, err := fmt.Fprintln(ctx.w, bar)
	return err
}

// writeBottomBar writes: └──────────────────...
func writeBottomBar(ctx *boxContext) error {
	_, err := fmt.Fprintln(ctx.w, "└"+strings.Repeat("─", ctx.barWidth-1))
	return err
}

// writeLine writes: │ content
func writeLine(ctx *boxContext, format string, args ...any) error {
	content := fmt.Sprintf(format, args...)
	_, err := fmt.Fprintf(ctx.w, "│ %s\n", content)
	return err
}

// buildBar constructs a decorative bar like "── title ────────..." or "├─ label ────────...".
func buildBar(prefix, middle string, width int) string {
	remaining := width - len(prefix) - len(middle)
	if remaining < 3 {
		remaining = 3
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
// deps.dev URL helpers (built inline to avoid importing infrastructure layer)
// ---------------------------------------------------------------------------

// buildDepsDevURL returns the deps.dev package overview page (no version).
func buildDepsDevURL(ecosystem, name string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	if eco == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s", eco, name)
}

// buildDepsDevVersionURL returns the deps.dev version-specific page.
func buildDepsDevVersionURL(ecosystem, name, version string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	if eco == "" || name == "" || version == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s/%s", eco, name, version)
}

// ---------------------------------------------------------------------------
// Section renderers
// ---------------------------------------------------------------------------

// writeBoxIdentity writes the Identity section (package, description, homepage, registry).
func writeBoxIdentity(ctx *boxContext) error {
	a := ctx.analysis
	displayPackage := ctx.entry.PURL
	if a != nil {
		if dp := a.DisplayPURL(); dp != "" && dp != ctx.entry.PURL {
			displayPackage = dp
		}
	}
	if err := writeLine(ctx, "Package: %s", displayPackage); err != nil {
		return err
	}
	if a != nil && a.Repository != nil && a.Repository.Description != "" {
		if desc := truncateDescription(a.Repository.Description); desc != "" {
			if err := writeLine(ctx, "Description: %s", desc); err != nil {
				return err
			}
		}
	}
	if a != nil && a.PackageLinks != nil {
		if a.PackageLinks.HomepageURL != "" {
			if err := writeLine(ctx, "  Homepage: %s", a.PackageLinks.HomepageURL); err != nil {
				return err
			}
		}
		if a.PackageLinks.RegistryURL != "" {
			if err := writeLine(ctx, "  Registry: %s", a.PackageLinks.RegistryURL); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeBoxOrigin writes the Origin section (source, relation, via).
// Returns nil without writing if no provenance info exists (direct PURL with no relation).
func writeBoxOrigin(ctx *boxContext) error {
	hasOrigin := ctx.entry.Source != domainaudit.SourceDirect ||
		ctx.entry.Relation != depparser.RelationUnknown ||
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
	if ctx.entry.Relation != depparser.RelationUnknown {
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

// writeBoxVerdict writes the Verdict section with emoji icon.
func writeBoxVerdict(ctx *boxContext) error {
	if err := writeSectionBar(ctx, "Verdict"); err != nil {
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

	// Repo state
	if a.RepoURL != "" {
		state := "Normal"
		if a.RepoState != nil {
			if a.RepoState.IsArchived {
				state = "📦 Archived"
			} else if a.RepoState.IsDisabled {
				state = "⛔ Disabled"
			} else if a.RepoState.IsFork {
				if a.RepoState.ForkSource != "" {
					state = fmt.Sprintf("Fork of %s", a.RepoState.ForkSource)
				} else {
					state = "Fork"
				}
			}
		}
		ghLine := fmt.Sprintf("GitHub: %s", state)
		if a.Repository != nil && a.Repository.StarsCount > 0 {
			ghLine += fmt.Sprintf(" (%d stars)", a.Repository.StarsCount)
		}
		lines = append(lines, ghLine)
	} else {
		lines = append(lines, "Hint: No repository URL found; Scorecard data unavailable")
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

	// Stable version
	if a.ReleaseInfo.StableVersion != nil && !a.ReleaseInfo.StableVersion.PublishedAt.IsZero() {
		stable := a.ReleaseInfo.StableVersion
		deprecated := ""
		if stable.IsDeprecated {
			deprecated = " ⚠️ [DEPRECATED]"
		}
		advCount := len(stable.Advisories)
		advText := fmt.Sprintf("Advisories: %d", advCount)
		if advCount > 0 {
			advText = fmt.Sprintf("⚠️ Advisories: %d", advCount)
		}
		lines = append(lines, fmt.Sprintf("Stable: %s (%s)  %s%s",
			stable.Version, stable.PublishedAt.Format(dateFormat), advText, deprecated))
		if stable.RegistryURL != "" {
			lines = append(lines, fmt.Sprintf("  ↳ Version Page: %s", stable.RegistryURL))
		}
		lines = append(lines, formatAdvisoryLines(stable.Advisories, eco, name, stable.Version)...)
	}

	// Pre-release
	if a.ReleaseInfo.PreReleaseVersion != nil && !a.ReleaseInfo.PreReleaseVersion.PublishedAt.IsZero() {
		pre := a.ReleaseInfo.PreReleaseVersion
		deprecated := ""
		if pre.IsDeprecated {
			deprecated = " ⚠️ [DEPRECATED]"
		}
		lines = append(lines, fmt.Sprintf("Pre-release: %s (%s)%s",
			pre.Version, pre.PublishedAt.Format(dateFormat), deprecated))
		if pre.RegistryURL != "" {
			lines = append(lines, fmt.Sprintf("  ↳ Version Page: %s", pre.RegistryURL))
		}
	}

	// Max semver
	if a.ReleaseInfo.MaxSemverVersion != nil && a.ReleaseInfo.MaxSemverVersion.Version != "" {
		maxv := a.ReleaseInfo.MaxSemverVersion
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
		if maxv.RegistryURL != "" {
			lines = append(lines, fmt.Sprintf("  ↳ Version Page: %s", maxv.RegistryURL))
		}
	}

	// Requested version
	if a.ReleaseInfo.RequestedVersion != nil && !a.ReleaseInfo.RequestedVersion.PublishedAt.IsZero() {
		rv := a.ReleaseInfo.RequestedVersion
		deprecated := ""
		if rv.IsDeprecated {
			deprecated = " ⚠️ [DEPRECATED]"
		}
		lines = append(lines, fmt.Sprintf("Requested: %s (%s)%s",
			rv.Version, rv.PublishedAt.Format(dateFormat), deprecated))
		if rv.RegistryURL != "" {
			lines = append(lines, fmt.Sprintf("  ↳ Version Page: %s", rv.RegistryURL))
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

// formatAdvisoryLines formats advisory entries with truncation.
// Shows up to maxDisplayAdvisories, then "... and N more → deps.dev URL".
func formatAdvisoryLines(advisories []analysispkg.Advisory, ecosystem, name, version string) []string {
	if len(advisories) == 0 {
		return nil
	}
	var lines []string
	limit := len(advisories)
	if limit > maxDisplayAdvisories {
		limit = maxDisplayAdvisories
	}
	for _, adv := range advisories[:limit] {
		lines = append(lines, fmt.Sprintf("  [%s] %s (%s)", adv.Source, adv.ID, adv.URL))
	}
	if len(advisories) > maxDisplayAdvisories {
		remaining := len(advisories) - maxDisplayAdvisories
		depsdevURL := buildDepsDevVersionURL(ecosystem, name, version)
		if depsdevURL != "" {
			lines = append(lines, fmt.Sprintf("  ... and %d more → %s", remaining, depsdevURL))
		} else {
			lines = append(lines, fmt.Sprintf("  ... and %d more", remaining))
		}
	}
	return lines
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
		if proj.Source != "" {
			return writeLine(ctx, "%s (source: %s / %s)", proj.Identifier, proj.Source, reqs[0].Source)
		}
		return writeLine(ctx, "%s", proj.Identifier)
	}

	// Project license
	if proj.Identifier != "" {
		if proj.Source != "" {
			if err := writeLine(ctx, "Project: %s (source: %s)", proj.Identifier, proj.Source); err != nil {
				return err
			}
		} else {
			if err := writeLine(ctx, "Project: %s", proj.Identifier); err != nil {
				return err
			}
		}
	} else if proj.IsNonStandard() && proj.Raw != "" {
		if err := writeLine(ctx, "Project: (non-standard raw=%s source=%s)", proj.Raw, proj.Source); err != nil {
			return err
		}
	} else if proj.IsZero() {
		if err := writeLine(ctx, "Project: (not detected)"); err != nil {
			return err
		}
	} else {
		if err := writeLine(ctx, "Project: (unclassified source=%s raw=%s)", proj.Source, proj.Raw); err != nil {
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
				if err := writeLine(ctx, "Requested Version: %s (source: %s)", strings.Join(ids, ", "), firstSource); err != nil {
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
					if err := writeLine(ctx, "Requested Version[%d]: %s (source: %s)", i, rl.Identifier, rl.Source); err != nil {
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

// writeBoxLinks writes the Links section (repository, registry, deps.dev, scorecard).
// Returns nil without writing if no URLs exist.
func writeBoxLinks(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}

	var lines []string

	if a.RepoURL != "" {
		repoURL := a.RepoURL
		if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
			repoURL = "https://" + repoURL
		}
		lines = append(lines, fmt.Sprintf("Repository: %s", repoURL))
	}
	if a.PackageLinks != nil && a.PackageLinks.RegistryURL != "" {
		lines = append(lines, fmt.Sprintf("Registry: %s", a.PackageLinks.RegistryURL))
	}

	// deps.dev link (package-level, no version)
	eco, name := packageEcoName(a)
	if depsdevURL := buildDepsDevURL(eco, name); depsdevURL != "" {
		lines = append(lines, fmt.Sprintf("deps.dev: %s", depsdevURL))
	}

	if a.ScorecardURL != "" {
		lines = append(lines, fmt.Sprintf("Scorecard: %s", a.ScorecardURL))
	}
	if a.ScorecardAPIURL != "" {
		lines = append(lines, fmt.Sprintf("Scorecard API: %s", a.ScorecardAPIURL))
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

	if err := writeTopBar(ctx); err != nil {
		return err
	}
	if err := writeBoxIdentity(ctx); err != nil {
		return err
	}
	if err := writeBoxOrigin(ctx); err != nil {
		return err
	}
	if err := writeBoxVerdict(ctx); err != nil {
		return err
	}
	if err := writeBoxEOL(ctx); err != nil {
		return err
	}
	if err := writeBoxHealth(ctx); err != nil {
		return err
	}
	if err := writeBoxReleases(ctx); err != nil {
		return err
	}
	if err := writeBoxLicenses(ctx); err != nil {
		return err
	}
	if err := writeBoxLinks(ctx); err != nil {
		return err
	}
	return writeBottomBar(ctx)
}

// renderBoxEntryError writes a minimal box for entries with nil analysis or errors.
func renderBoxEntryError(ctx *boxContext) error {
	if err := writeTopBar(ctx); err != nil {
		return err
	}
	if err := writeLine(ctx, "Package: %s", ctx.entry.PURL); err != nil {
		return err
	}
	if ctx.entry.Via != "" {
		if err := writeLine(ctx, "Via: %s", ctx.entry.Via); err != nil {
			return err
		}
	}
	if err := writeSectionBar(ctx, "Verdict"); err != nil {
		return err
	}
	icon := verdictIcon(ctx.entry.Verdict)
	label := verdictLabel(ctx.entry.Verdict)
	if ctx.entry.ErrorMsg != "" {
		if err := writeLine(ctx, "%s %s (error: %s)", icon, label, ctx.entry.ErrorMsg); err != nil {
			return err
		}
	} else {
		if err := writeLine(ctx, "%s %s", icon, label); err != nil {
			return err
		}
	}
	return writeBottomBar(ctx)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// packageEcoName extracts ecosystem and package name suitable for deps.dev URLs.
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
	return parsed.GetEcosystem(), parsed.GetPackageName()
}

