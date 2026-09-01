// Package analysis defines the core domain models and value objects
package analysis

import (
	"time"
)

// ResolvedLicense represents normalized license information used at project
// level (Analysis.ProjectLicense) and requested-version level
// (Analysis.RequestedVersionLicense). The data model is documented in
// docs/adr/0019-license-expression-of-truth.md.
//
// Fields:
//   - Expression: canonical SPDX expression string, or "" / "NOASSERTION".
//     Expression == "" means uzomuzo could not parse / recognize any SPDX
//     content; Expression == "NOASSERTION" preserves the SPDX 2.3 §A.1.5
//     sentinel emitted by upstream. Compound expressions (AND / OR / WITH /
//     +) survive in this single string — never split across slice entries.
//   - Source: provenance (see license_sources.go constants).
//   - Raw: upstream-original string verbatim. NEVER overwritten by the
//     normalizer; preserved for audit trail and SBOM `name` fallback when
//     Expression is empty (per CycloneDX 1.6 / SPDX 2.3 LicenseRef path).
//
// Zero value (all empty) means no license detected.
type ResolvedLicense struct {
	Expression string
	Source     string
	Raw        string
}

// IsZero reports whether this license carries no information at all —
// neither a recognized SPDX expression nor an upstream original to fall back
// on. A zero ResolvedLicense means we did not detect ANY project / version
// license data.
func (r ResolvedLicense) IsZero() bool {
	return r.Expression == "" && r.Source == "" && r.Raw == ""
}

// IsNonStandard reports whether the upstream provided license information,
// but it could not be mapped to a canonical SPDX expression (non-SPDX /
// ambiguous / proprietary wording). It intentionally excludes the pure zero
// case (no data) and any successfully recognized SPDX expression
// (including "NOASSERTION").
//
// Detection rules:
//   - NOT IsZero (we have some data)
//   - Expression is empty (normalization could not produce SPDX)
//   - Source is one of the known non-standard / raw indicators:
//   - LicenseSourceDepsDevProjectNonStandard
//   - LicenseSourceGitHubProjectNonStandard
//   - LicenseSourceDepsDevVersionRaw
//   - LicenseSourceGitHubVersionRaw (reserved / future)
//   - LicenseSourceMavenPOMNonStandard
//   - LicenseSourceClearlyDefinedNonStandard
//
// Notes:
//   - A promoted or fallback SPDX (derived-from-version / project-fallback)
//     is NEVER non-standard.
//   - "NOASSERTION" is a recognized SPDX value, not a non-standard one.
func (r ResolvedLicense) IsNonStandard() bool {
	if r.IsZero() {
		return false
	}
	if r.Expression != "" { // any recognized SPDX (including "NOASSERTION") => standard
		return false
	}
	switch r.Source {
	case LicenseSourceDepsDevProjectNonStandard,
		LicenseSourceGitHubProjectNonStandard,
		LicenseSourceDepsDevVersionRaw,
		LicenseSourceGitHubVersionRaw,
		LicenseSourceMavenPOMNonStandard,
		LicenseSourceClearlyDefinedNonStandard:
		return true
	default:
		return false
	}
}

// IsUsableSPDX reports whether Expression carries an SPDX value that downstream
// consumers can act on (policy checks, compliance reports, leaf inspection) —
// excluding the SPDX 2.3 §A.1.5 "NOASSERTION" sentinel, which is *recognized*
// (not non-standard) but conveys "upstream refused to assert" rather than a
// real license. Use this instead of inlining `Expression != "" && Expression
// != "NOASSERTION"`: the predicate is a property of ResolvedLicense and must
// stay aligned with `IsZero` / `IsNonStandard` (per ADR-0019).
//
// Compound expressions like `"MIT OR Apache-2.0"` return true; single-leaf
// canonical SPDX returns true; `""` and `"NOASSERTION"` return false.
func (r ResolvedLicense) IsUsableSPDX() bool {
	return r.Expression != "" && r.Expression != "NOASSERTION"
}

// Repository represents a code repository being analyzed
type Repository struct {
	URL         string
	Owner       string
	Name        string
	StarsCount  int
	ForksCount  int
	Language    string
	Description string
	LastCommit  time.Time
	// DefaultBranch is the canonical default branch name (e.g. main, master) fetched via GitHub GraphQL.
	// It enables downstream fetchers (README, go.mod, etc.) to avoid guessing common branch names.
	DefaultBranch string
	// Summary is a short, UI-ready, normalized one-line description (≤200 runes).
	// Per-source provenance:
	//   - GitHub repos:      GraphQL repository.description (already short).
	//   - deps.dev Project:  project.description (repo-level, short).
	//   - PyPI packages:     info.summary from PyPI JSON API (overrides above for ecosystem=pypi).
	// Empty when no source provided a usable value. Description (above) is preserved unchanged
	// for consumers that want the raw upstream value; see NormalizeSummary for the rules.
	Summary string
	// Topics holds GitHub repository topics (already-lowercased tags) returned by the
	// repositoryTopics GraphQL connection (capped at 20). Sentinel values:
	//   - nil       : not fetched (no GitHub token, non-GitHub host, or fetch failed)
	//   - []string{}: fetched successfully, repository has zero topics configured
	//   - non-empty : fetched topics in GitHub-returned order, deduplicated
	Topics []string
}

// Package represents a package being analyzed
type Package struct {
	PURL      string
	Ecosystem string
	Version   string
}

// PackageLinks groups package-level (version-agnostic) canonical links.
// Zero values are ignored by consumers.
type PackageLinks struct {
	HomepageURL string // canonical project homepage
	RegistryURL string // official registry landing page for the package (no version)
}

// ReleaseInfo represents release information for a repository
type ReleaseInfo struct {
	StableVersion     *VersionDetail
	PreReleaseVersion *VersionDetail
	MaxSemverVersion  *VersionDetail
	RequestedVersion  *VersionDetail
}

// VersionDetail represents details of a specific version
type VersionDetail struct {
	Version      string
	PublishedAt  time.Time
	IsPrerelease bool
	// IsDeprecated indicates the upstream has deprecated / retracted / yanked this specific version.
	// Propagated from deps.dev API Version.IsDeprecated.
	//
	// Consumed as a fallback EOL signal by eolevaluator's applyDepsDevDeprecated rule
	// for ecosystems without authoritative ecosystem-specific rules (golang, gem, pub,
	// hex, conan). For ecosystems with authoritative rules (npm, NuGet, Packagist,
	// Maven, PyPI, cargo), this field remains informational and the ecosystem-specific
	// rule's verdict takes precedence via short-circuiting in the rule chain.
	IsDeprecated bool
	// RegistryURL points to the registry page specific to this version (flattened from former VersionLinks)
	RegistryURL string
	// Advisories lists security advisories (GHSA / CVE / OSV / other) affecting this version.
	// We collect all advisoryKeys (no prefix filtering) to avoid missing important data.
	Advisories []Advisory
}

// Advisory represents a single security advisory reference.
// Source is a normalized identifier (GHSA, CVE, OSV, OTHER).
type Advisory struct {
	ID     string
	Source string
	URL    string
	// Title is the advisory summary (e.g., "SQL Injection in foo"). Empty if not fetched.
	Title string
	// CVSS3Score is the CVSS v3 base score (0.0–10.0). Zero means unknown/not fetched.
	CVSS3Score float64
	// Severity is derived from CVSS3Score: NONE/LOW/MEDIUM/HIGH/CRITICAL. Empty means unknown.
	Severity string
	// Relation indicates whether this advisory affects the package directly or via a transitive dependency.
	// Values: "DIRECT", "TRANSITIVE", or "" (unknown/legacy — treated as direct).
	Relation string
	// DependencyName identifies the transitive dependency this advisory originates from.
	// Format: "name@version" (e.g., "qs@6.5.5"). Empty for direct advisories.
	DependencyName string
}

// Advisory Relation constants.
const (
	AdvisoryRelationDirect     = "DIRECT"
	AdvisoryRelationTransitive = "TRANSITIVE"
)

// DirectAdvisories returns advisories that affect the package directly (Relation is DIRECT or empty).
func (vd *VersionDetail) DirectAdvisories() []Advisory {
	var result []Advisory
	for _, a := range vd.Advisories {
		if a.Relation == AdvisoryRelationDirect || a.Relation == "" {
			result = append(result, a)
		}
	}
	return result
}

// TransitiveAdvisories returns advisories that affect transitive dependencies.
func (vd *VersionDetail) TransitiveAdvisories() []Advisory {
	var result []Advisory
	for _, a := range vd.Advisories {
		if a.Relation == AdvisoryRelationTransitive {
			result = append(result, a)
		}
	}
	return result
}

// DirectAdvisoryCount returns the count of direct advisories.
func (vd *VersionDetail) DirectAdvisoryCount() int {
	count := 0
	for _, a := range vd.Advisories {
		if a.Relation == AdvisoryRelationDirect || a.Relation == "" {
			count++
		}
	}
	return count
}

// TransitiveAdvisoryCount returns the count of transitive advisories.
func (vd *VersionDetail) TransitiveAdvisoryCount() int {
	count := 0
	for _, a := range vd.Advisories {
		if a.Relation == AdvisoryRelationTransitive {
			count++
		}
	}
	return count
}

// MaxTransitiveCVSS3 returns the highest CVSS3 score among transitive advisories, or 0 if none.
func (vd *VersionDetail) MaxTransitiveCVSS3() float64 {
	var max float64
	for _, a := range vd.Advisories {
		if a.Relation == AdvisoryRelationTransitive && a.CVSS3Score > max {
			max = a.CVSS3Score
		}
	}
	return max
}

// SeverityFromCVSS3 maps a CVSS v3 base score to a severity label per the CVSS v3 specification.
// Returns empty string for zero (unknown/not fetched).
func SeverityFromCVSS3(score float64) string {
	switch {
	case score <= 0:
		return ""
	case score <= 3.9:
		return "LOW"
	case score <= 6.9:
		return "MEDIUM"
	case score <= 8.9:
		return "HIGH"
	default:
		return "CRITICAL"
	}
}

// MaxCVSS3 returns the highest CVSS3 score among advisories, or 0 if none have severity data.
func (vd *VersionDetail) MaxCVSS3() float64 {
	var max float64
	for _, a := range vd.Advisories {
		if a.CVSS3Score > max {
			max = a.CVSS3Score
		}
	}
	return max
}

// HighSeverityAdvisoryCount returns the count of advisories with CVSS3 >= threshold.
func (vd *VersionDetail) HighSeverityAdvisoryCount(threshold float64) int {
	count := 0
	for _, a := range vd.Advisories {
		if a.CVSS3Score >= threshold {
			count++
		}
	}
	return count
}

// UnknownSeverityAdvisoryCount returns the count of advisories without severity data (CVSS3Score == 0).
func (vd *VersionDetail) UnknownSeverityAdvisoryCount() int {
	count := 0
	for _, a := range vd.Advisories {
		if a.CVSS3Score <= 0 {
			count++
		}
	}
	return count
}

// LatestVersionDetail returns the highest-priority VersionDetail per priority order:
// Stable > MaxSemver > PreRelease > Requested. Returns nil if no version exists.
func (ri *ReleaseInfo) LatestVersionDetail() *VersionDetail {
	if ri == nil {
		return nil
	}
	if ri.StableVersion != nil {
		return ri.StableVersion
	}
	if ri.MaxSemverVersion != nil {
		return ri.MaxSemverVersion
	}
	if ri.PreReleaseVersion != nil {
		return ri.PreReleaseVersion
	}
	if ri.RequestedVersion != nil {
		return ri.RequestedVersion
	}
	return nil
}

// LatestAdvisories returns (count, advisories) for the "latest" version per priority order:
// Stable > MaxSemver > PreRelease > Requested. If Stable exists it is always chosen even if zero length.
func (ri *ReleaseInfo) LatestAdvisories() (int, []Advisory) {
	vd := ri.LatestVersionDetail()
	if vd == nil {
		return 0, nil
	}
	return len(vd.Advisories), vd.Advisories
}

// CommitStats represents commit statistics
type CommitStats struct {
	Total       int
	BotCommits  int
	UserCommits int
	BotRatio    float64
	UserRatio   float64
}

// Registry names recorded in RegistryState.Registry.
const (
	// RegistryPyPI identifies pypi.org.
	RegistryPyPI = "PyPI"
	// RegistryCrates identifies crates.io.
	RegistryCrates = "crates.io"
)

// RegistryState captures package-level facts asserted by the package registry
// itself, as opposed to RepoState which describes the source repository host.
// The two can disagree: a repository may be actively developed while the
// registry has withdrawn every published release (e.g. conda on PyPI).
//
// A nil pointer means the facts were not obtained — the ecosystem is not one we
// ask, the client is unwired, the PURL was unparseable or carried a namespace,
// the package was not found, or the request failed. A non-nil value always means
// the registry answered, including when nothing is yanked.
type RegistryState struct {
	// AllReleasesYanked is true when the registry reports that every published
	// release of the package is yanked. It is a distribution-withdrawal signal
	// ("do not install this from here"), not end-of-life. See ADR-0022.
	AllReleasesYanked bool
	// Registry names the asserting registry: RegistryPyPI or RegistryCrates.
	Registry string
	// Reason is the registry-provided yank reason, normalized to a single line.
	// Empty when the registry does not carry one: crates.io never does, and
	// PyPI's yanked_reason may be null.
	Reason string
	// Reference is a registry UI URL where the fact can be verified.
	Reference string
}

// RepoState represents repository state information
type RepoState struct {
	LatestHumanCommit   *time.Time
	DaysSinceLastCommit int
	CommitStats         *CommitStats
	// IsArchived indicates the repository owner explicitly archived the repository.
	// Archived repos are intentionally put into read-only maintenance mode; code remains visible
	// but pushes, issues, and pull requests are typically disabled. This is a strong signal that
	// active development has ceased, yet the state is owner-driven and not inherently a policy / risk action.
	IsArchived bool
	// IsDisabled indicates GitHub (the platform) has disabled the repository (e.g. ToS / abuse / DMCA / policy action).
	// This is a platform-enforced state and is a higher severity signal than IsArchived: the project should be
	// treated as unusable or high-risk for new dependencies until reinstated. Users cannot self-mark this state.
	IsDisabled bool
	// IsFork flags that the repository is a fork (helpful for judging maintenance independence and original activity).
	IsFork bool
	// ForkSource is the immediate fork parent repository in "owner/repo" format (GitHub GraphQL parent.nameWithOwner).
	// Empty when IsFork is false, or when the fork's parent is private/inaccessible, or when GitHub data is unavailable.
	// Useful for LLM-based health assessment to suggest evaluating the upstream project instead of the fork.
	ForkSource string
}
