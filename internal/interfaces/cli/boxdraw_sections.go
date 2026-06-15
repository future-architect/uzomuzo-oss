package cli

import (
	"fmt"
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
			// Description is free text without a label prefix; use a fixed
			// continuation indent (2 spaces) to avoid misdetecting ": " in
			// natural-language text as a label pattern.
			maxWidth := ctx.barWidth - 2 // subtract "│ " prefix width
			if maxWidth > 0 && utf8.RuneCountInString(desc) > maxWidth {
				for _, line := range wrapContentWithIndent(desc, maxWidth, 2) {
					if _, err := fmt.Fprintf(ctx.w, "│ %s\n", line); err != nil {
						return fmt.Errorf("failed to write box line: %w", err)
					}
				}
			} else {
				if _, err := fmt.Fprintf(ctx.w, "│ %s\n", desc); err != nil {
					return fmt.Errorf("failed to write box line: %w", err)
				}
			}
		}
	}
	return nil
}

// writeBoxSignals writes the Signals section — data points that directly
// influenced the lifecycle verdict. Returns nil if no signals exist.
func writeBoxSignals(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}
	lr := a.GetLifecycleResult()
	if lr == nil || len(lr.Signals) == 0 {
		return nil
	}
	if err := writeSectionBar(ctx, "Signals"); err != nil {
		return err
	}
	for _, s := range lr.Signals {
		label := signalDisplayName(s.Name)
		if s.Role == analysispkg.SignalAbsent {
			if err := writeLine(ctx, "%s: (unavailable)", label); err != nil {
				return err
			}
		} else {
			if err := writeLine(ctx, "%s: %s", label, s.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// signalDisplayName maps machine signal names to human-readable labels.
func signalDisplayName(name string) string {
	switch name {
	case analysispkg.SignalEOLSource:
		return "EOL Source"
	case analysispkg.SignalEOLScheduledDate:
		return "EOL Scheduled Date"
	case analysispkg.SignalRepoArchived:
		return "Repo Archived"
	case analysispkg.SignalRepoDisabled:
		return "Repo Disabled"
	case analysispkg.SignalMaintainedScore:
		return "Maintained Score"
	case analysispkg.SignalLastHumanCommit:
		return "Last Human Commit"
	case analysispkg.SignalRecentStableRelease:
		return "Recent Stable Release"
	case analysispkg.SignalRecentPreRelease:
		return "Recent Pre-Release"
	case analysispkg.SignalAdvisoryCount:
		return "Advisories"
	case analysispkg.SignalMaxAdvisorySeverity:
		return "Max Advisory Severity"
	case analysispkg.SignalDaysSinceRelease:
		return "Days Since Release"
	case analysispkg.SignalEcosystemDelivery:
		return "Ecosystem Delivery"
	default:
		return name
	}
}

// buildIntegrityMediumSignals lists medium-tier signals that are only displayed
// when evaluated (Role == SignalUsed). All other signals are Critical/High
// and always shown regardless of evaluation status.
var buildIntegrityMediumSignals = map[string]bool{
	analysispkg.SignalPinnedDependencies: true,
}

// buildSignalCompactScore extracts a compact score display from a Signal.
// Returns the numeric part of "N/10", "10" for "verified", or "—" for absent.
func buildSignalCompactScore(s analysispkg.Signal) string {
	if s.Role == analysispkg.SignalAbsent {
		return "—"
	}
	if s.Value == "verified" {
		return "10"
	}
	if idx := strings.Index(s.Value, "/"); idx > 0 {
		return s.Value[:idx]
	}
	return s.Value
}

// writeBoxBuildIntegrity writes the Build Integrity section in a compact
// 2-column layout. Critical/High signals are always shown (including —).
// Medium signals are shown only when evaluated. Returns nil without writing
// if no build integrity data exists, label is Ungraded, or verdict is replace
// (EOL/archived packages).
func writeBoxBuildIntegrity(ctx *boxContext) error {
	a := ctx.analysis
	if a == nil {
		return nil
	}
	// Hide Build Integrity for EOL/archived packages (verdict=replace).
	if ctx.entry.Verdict == domainaudit.VerdictReplace {
		return nil
	}
	br := a.GetBuildHealthResult()
	if br == nil || br.Label == string(analysispkg.BuildLabelUngraded) || br.Label == "" {
		return nil
	}

	var evaluated int
	for _, s := range br.Signals {
		if s.Role == analysispkg.SignalUsed {
			evaluated++
		}
	}
	total := len(br.Signals)

	if err := writeSectionBar(ctx, "Build Integrity"); err != nil {
		return err
	}

	// Status line with icon.
	icon := buildIntegrityIcon(analysispkg.BuildIntegrityLabel(br.Label))
	scoreStr := br.Meta["score"]
	statusLine := br.Label
	if scoreStr != "" && scoreStr != analysispkg.ScoreUngraded {
		statusLine = fmt.Sprintf("%s %s/10 (%d/%d)", br.Label, scoreStr, evaluated, total)
	}
	if err := writeLine(ctx, "%s %s", icon, statusLine); err != nil {
		return err
	}

	// Filter: Critical/High always shown; Medium only when evaluated.
	var visible []analysispkg.Signal
	for _, s := range br.Signals {
		if buildIntegrityMediumSignals[s.Name] && s.Role != analysispkg.SignalUsed {
			continue
		}
		visible = append(visible, s)
	}

	// Render in 2-column layout.
	const colW = 19
	for i := 0; i < len(visible); i += 2 {
		lLabel := buildSignalDisplayName(visible[i].Name)
		lScore := buildSignalCompactScore(visible[i])

		if i+1 < len(visible) {
			rLabel := buildSignalDisplayName(visible[i+1].Name)
			rScore := buildSignalCompactScore(visible[i+1])
			if err := writeLine(ctx, "%-*s %2s  %-*s %2s", colW, lLabel, lScore, colW, rLabel, rScore); err != nil {
				return err
			}
		} else {
			if err := writeLine(ctx, "%-*s %2s", colW, lLabel, lScore); err != nil {
				return err
			}
		}
	}

	if a.ScorecardURL != "" {
		if err := writeLine(ctx, "→ %s", a.ScorecardURL); err != nil {
			return err
		}
	}
	return nil
}

// buildIntegrityIcon returns the status icon for a build integrity label.
func buildIntegrityIcon(label analysispkg.BuildIntegrityLabel) string {
	switch label {
	case analysispkg.BuildLabelHardened:
		return "✅"
	case analysispkg.BuildLabelModerate:
		return "⚠️"
	case analysispkg.BuildLabelWeak:
		return "🔴"
	default:
		return "🔍"
	}
}

// buildSignalDisplayName maps build signal machine names to compact display labels.
func buildSignalDisplayName(name string) string {
	switch name {
	case analysispkg.SignalDangerousWorkflow:
		return "Dangerous Workflow"
	case analysispkg.SignalBranchProtection:
		return "Branch Protection"
	case analysispkg.SignalCodeReview:
		return "Code Review"
	case analysispkg.SignalTokenPermissions:
		return "Token Permissions"
	case analysispkg.SignalBinaryArtifacts:
		return "Binary Artifacts"
	case analysispkg.SignalPinnedDependencies:
		return "Pinned Deps"
	default:
		return name
	}
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

// writeBoxVerdict writes lifecycle verdict inline (no section bar).
// Format: "icon Label: reason" on a single line (word-wrapped if long).
// Displayed immediately after identity, before any section bars.
func writeBoxVerdict(ctx *boxContext) error {
	icon := verdictIcon(ctx.entry.Verdict)
	label := verdictLabel(ctx.entry.Verdict)
	reason := ""

	// Use lifecycle label and reason if available
	if ctx.analysis != nil {
		if lr := ctx.analysis.GetLifecycleResult(); lr != nil {
			label = lr.Label
			reason = lr.Reason
		}
	}

	if reason != "" {
		if err := writeLine(ctx, "%s %s: %s", icon, label, reason); err != nil {
			return err
		}
	} else {
		if err := writeLine(ctx, "%s %s", icon, label); err != nil {
			return err
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
		}
		if a.RepoState.IsDisabled {
			lines = append(lines, "⛔ Disabled")
		}
		if a.RepoState.IsFork {
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
		scoreLine := fmt.Sprintf("Scorecard Overall: %.*f/10", scorePrecision, a.OverallScore)

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
		advText := advisoryCountText(stable)
		if !stable.PublishedAt.IsZero() {
			lines = append(lines, fmt.Sprintf("Stable: %s (%s)%s%s",
				stable.Version, stable.PublishedAt.Format(dateFormat), advText, deprecated))
		} else {
			lines = append(lines, fmt.Sprintf("Stable: %s%s%s",
				stable.Version, advText, deprecated))
		}
		lines = append(lines, formatAdvisoryLines(stable.DirectAdvisories())...)
		lines = append(lines, formatTransitiveAdvisoryLines(stable.TransitiveAdvisories())...)
		// deps.dev link after all advisories (direct + transitive are both visible on this page)
		if len(stable.Advisories) > 0 {
			if depsdevURL := commonlinks.BuildDepsDevVersionURL(eco, name, stable.Version); depsdevURL != "" {
				lines = append(lines, fmt.Sprintf("  → %s", depsdevURL))
			}
		}
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

	// Requested version (skip if same as stable)
	if a.ReleaseInfo.RequestedVersion != nil && a.ReleaseInfo.RequestedVersion.Version != "" {
		rv := a.ReleaseInfo.RequestedVersion
		if rv.Version != stableVer {
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

// packageEcoName extracts ecosystem and the canonical single-segment package
// name suitable for deps.dev (and other registry) URLs. Maven joins groupId
// and artifactId with `:` (deps.dev / Maven Central convention); other
// namespaced ecosystems join with `/`. The returned `name` is unescaped —
// the URL builder is responsible for percent-encoding.
//
// Uses the EffectivePURL (resolved PURL) when available, falling back to
// the original PURL.
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
	eco := parsed.Ecosystem()
	ns := parsed.Namespace()
	name = parsed.Name()
	if ns != "" {
		sep := "/"
		if strings.EqualFold(eco, "maven") {
			sep = ":"
		}
		name = ns + sep + name
	}
	return eco, name
}
