package eolevaluator

import (
	"log/slog"
	"net/url"
	"strings"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	eoltext "github.com/future-architect/uzomuzo-oss/internal/infrastructure/eoltext"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
)

// Normalized NuGet deprecation reason strings (lowercase).
// See: https://learn.microsoft.com/nuget/api/registration-base-url-resource#package-deprecation
const (
	nugetReasonLegacy       = "legacy"
	nugetReasonCriticalBugs = "criticalbugs"
	nugetReasonOther        = "other"
)

func parseComposerFromPURL(purl string) (string, string) {
	// Supports both pkg:composer/vendor/name@ver and pkg:packagist/vendor/name@ver forms
	s := strings.TrimSpace(purl)
	if s == "" {
		return "", ""
	}
	if !strings.HasPrefix(s, "pkg:") {
		return "", ""
	}
	s = strings.TrimPrefix(s, "pkg:")
	// s now starts with type/...
	slash := strings.IndexByte(s, '/')
	if slash < 0 {
		return "", ""
	}
	typ := s[:slash]
	rest := s[slash+1:]
	if !strings.EqualFold(typ, "composer") && !strings.EqualFold(typ, "packagist") {
		return "", ""
	}
	// Cut off version/qualifiers/subpath: @version ?qualifiers #subpath
	cut := len(rest)
	if i := strings.IndexByte(rest, '@'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(rest, '#'); i >= 0 && i < cut {
		cut = i
	}
	rest = rest[:cut]
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return "", ""
	}
	vendor := parts[0]
	name := parts[1]
	if vendor == "" || name == "" {
		return "", ""
	}
	return vendor, name
}

// parseNuGetIDFromPURL extracts the NuGet package ID from a PURL like:
// pkg:nuget/Package.Id@1.2.3 or pkg:nuget/Package.Id
func parseNuGetIDFromPURL(purl string) string {
	s := strings.TrimSpace(purl)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "pkg:") {
		return ""
	}
	s = strings.TrimPrefix(s, "pkg:")
	slash := strings.IndexByte(s, '/')
	if slash < 0 {
		return ""
	}
	typ := s[:slash]
	rest := s[slash+1:]
	if !strings.EqualFold(typ, "nuget") {
		return ""
	}
	cut := len(rest)
	if i := strings.IndexByte(rest, '@'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(rest, '#'); i >= 0 && i < cut {
		cut = i
	}
	id := strings.TrimSpace(rest[:cut])
	return id
}

// parseMavenFromPURL extracts (groupId, artifactId, version) from a PURL like:
//
//	pkg:maven/group.id/artifact-id@1.2.3
//
// Returns empty strings when not a Maven PURL or when components are missing.
func parseMavenFromPURL(purl string) (string, string, string) {
	s := strings.TrimSpace(purl)
	if s == "" || !strings.HasPrefix(s, "pkg:") {
		return "", "", ""
	}
	s = strings.TrimPrefix(s, "pkg:")
	slash := strings.IndexByte(s, '/')
	if slash < 0 {
		return "", "", ""
	}
	typ := s[:slash]
	rest := s[slash+1:]
	if !strings.EqualFold(typ, "maven") {
		return "", "", ""
	}
	// Cut at qualifiers/subpath; keep @version to parse later
	cut := len(rest)
	if i := strings.IndexByte(rest, '?'); i >= 0 && i < cut {
		cut = i
	}
	if i := strings.IndexByte(rest, '#'); i >= 0 && i < cut {
		cut = i
	}
	core := rest[:cut]
	// Split group/artifact and optional @version
	var coords, ver string
	if i := strings.IndexByte(core, '@'); i >= 0 {
		coords = core[:i]
		ver = core[i+1:]
	} else {
		coords = core
		ver = ""
	}
	parts := strings.Split(coords, "/")
	if len(parts) < 2 {
		return "", "", ""
	}
	g := strings.TrimSpace(parts[0])
	a := strings.TrimSpace(parts[1])
	v := strings.TrimSpace(ver)
	if g == "" || a == "" || v == "" {
		return "", "", ""
	}
	return g, a, v
}

// decideNpmEOL maps npm DeprecationInfo to EOL decision and evidences for stable version.
func decideNpmEOL(pkgID, version string, info *npmjs.DeprecationInfo) (state domain.EOLState, successor string, evidences []domain.EOLEvidence) {
	if info == nil {
		return domain.EOLNotEOL, "", nil
	}
	successor = info.Successor
	// Evidence should include the exact registry endpoint used for lifecycle classification (Reference)
	// and provide a human-friendly UI URL in the summary for quick verification.
	regURL := "https://registry.npmjs.org/" + url.PathEscape(pkgID)
	if info.Unpublished {
		evidences = append(evidences, domain.EOLEvidence{
			Source:     "npmjs",
			Reference:  regURL,
			Confidence: 0.95,
			Summary:    "Stable version is unpublished in npm registry",
		})
		return domain.EOLEndOfLife, successor, evidences
	}
	if info.Deprecated {
		evidences = append(evidences, domain.EOLEvidence{
			Source:     "npmjs",
			Reference:  regURL,
			Confidence: 0.9,
			Summary:    "Stable version is deprecated in npm registry",
		})
		return domain.EOLEndOfLife, successor, evidences
	}
	// Not deprecated/unpublished: no evidence to avoid noise
	return domain.EOLNotEOL, "", nil
}

func normalizeReasons(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.ToLower(strings.TrimSpace(r))
		if r != "" {
			out = append(out, r)
		}
	}
	return out
}

func contains(arr []string, want string) bool {
	for _, s := range arr {
		if s == want {
			return true
		}
	}
	return false
}

// Removed local strong phrase list; unified detection via eoltext.DetectGeneric.

// decideNuGetEOL maps NuGet deprecation info to EOL decision and evidences.
//
// DDD Layer: Infrastructure (successor)
// Inputs:
//   - id: NuGet package ID (as appears on nuget.org)
//   - info: deprecation metadata fetched from NuGet Registration API
//
// Outputs:
//   - state: EOLEndOfLife or EOLNotEOL based on policy
//   - successor: alternate package ID when applicable
//   - evidences: one evidence describing the decision (EOL or warning)
//
// decideNuGetEOL maps NuGet deprecation info to our EOL decision and evidence.
//
// Sources (authoritative NuGet docs):
//   - Registration API "Package deprecation" schema and reasons list
//     https://learn.microsoft.com/nuget/api/registration-base-url-resource#package-deprecation
//     Reasons (case-insensitive): Legacy, CriticalBugs, Other
//   - nuget.org deprecation workflow and semantics (UI and client behavior)
//     https://learn.microsoft.com/nuget/nuget-org/deprecate-packages
//
// Policy implemented (project-specific):
//   - CriticalBugs => EOL with confidence 1.0
//   - Legacy with AlternatePackageID => EOL with successor, confidence 0.9
//   - Legacy without AlternatePackageID => not EOL, warning evidence (confidence 0.7)
//   - Other => not EOL, warning evidence (confidence 0.5 by default)
//     If message contains strong terms ("unmaintained", "deprecated permanently",
//     "no longer maintained"), warning evidence confidence is 0.8
//
// Notes:
//   - Reasons are normalized to lowercase and matched using constants above.
//   - Evidence.Reference uses the nuget.org package URL for both EOL and warnings for easier triage.
//   - This function is pure (no I/O) and intended to be unit tested in isolation.
func decideNuGetEOL(id string, info *nuget.DeprecationInfo) (state domain.EOLState, successor string, evidences []domain.EOLEvidence) {
	state = domain.EOLNotEOL
	if info == nil {
		return
	}
	rs := normalizeReasons(info.Reasons)

	// Use NuGet Registration API endpoint for traceable Reference, plus UI URL in Summary.
	idLower := strings.ToLower(id)
	regURL := "https://api.nuget.org/v3/registration5-semver2/" + url.PathEscape(idLower) + "/index.json"
	uiURL := "https://www.nuget.org/packages/" + id

	// Short-circuit: if upstream provides a clear successor, we consider it EOL regardless of reason label.
	// This covers HTML-fallback detection where reasons may be generic ("Other").
	if info.AlternatePackageID != "" {
		slog.Debug("eol: nuget successor present -> EOL", "id", id, "successor", info.AlternatePackageID)
		state = domain.EOLEndOfLife
		successor = info.AlternatePackageID
		evidences = append(evidences, domain.EOLEvidence{
			Source:     "NuGet",
			Summary:    "Package deprecated with successor. UI: " + uiURL,
			Reference:  regURL,
			Confidence: 0.9,
		})
		return
	}

	// CriticalBugs => definitive EOL
	if contains(rs, nugetReasonCriticalBugs) {
		slog.Debug("eol: nuget CriticalBugs -> EOL", "id", id)
		state = domain.EOLEndOfLife
		evidences = append(evidences, domain.EOLEvidence{
			Source:     "NuGet",
			Summary:    "Package deprecated: CriticalBugs. UI: " + uiURL,
			Reference:  regURL,
			Confidence: 1.0,
		})
		return
	}

	// Legacy without successor => warn
	if contains(rs, nugetReasonLegacy) {
		// Legacy without successor => warn
		slog.Debug("eol: nuget Legacy(no successor) -> warn only", "id", id)
		evidences = append(evidences, domain.EOLEvidence{
			Source:     "NuGet",
			Summary:    "Package deprecated: Legacy (no successor). UI: " + uiURL,
			Reference:  regURL,
			Confidence: 0.7,
		})
		return
	}

	// Other => warning; optionally escalate if message indicates strong phrases (shared matcher)
	if contains(rs, nugetReasonOther) {
		res := eoltext.DetectLifecycle(eoltext.LifecycleDetectOpts{Source: eoltext.SourceShortMessage, PackageName: id, Text: info.Message})
		strong := res.Matched && res.Kind != eoltext.KindContextual // treat strong/explicit as higher confidence
		if strong {
			slog.Debug("eol: nuget Other(strong wording) -> warn", "id", id)
			evidences = append(evidences, domain.EOLEvidence{
				Source:     "NuGet",
				Summary:    "Package deprecated: Other (strong wording). UI: " + uiURL,
				Reference:  regURL,
				Confidence: 0.8,
			})
		} else {
			slog.Debug("eol: nuget Other -> warn", "id", id)
			evidences = append(evidences, domain.EOLEvidence{
				Source:     "NuGet",
				Summary:    "Package deprecated: Other. UI: " + uiURL,
				Reference:  regURL,
				Confidence: 0.5,
			})
		}
	}

	return
}

// hasPyPIInactiveClassifier returns true if the classifiers contain the
// explicit inactivity (Development Status :: 7 - Inactive) signal which we treat
// as an explicit EOL (authoritative self-declared inactivity).
func hasPyPIInactiveClassifier(classifiers []string) bool {
	if len(classifiers) == 0 {
		return false
	}
	for _, c := range classifiers {
		if strings.EqualFold(strings.TrimSpace(c), "Development Status :: 7 - Inactive") {
			return true
		}
	}
	return false
}
