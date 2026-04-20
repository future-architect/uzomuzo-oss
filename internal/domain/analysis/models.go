// Package analysis defines the core domain models and value objects
package analysis

import (
	"time"
)

// ResolvedLicense represents normalized license information used at project level (Analysis.ProjectLicense)
// and requested-version level (Analysis.RequestedVersionLicenses entries).
//
// Fields:
//   - Identifier: normalized canonical SPDX identifier when recognized; otherwise best-effort normalized token (may be empty for non-standard placeholders)
//   - Source: provenance (see license_sources.go constants, e.g. LicenseSourceDepsDevProjectSPDX, LicenseSourceDepsDevProjectNonStandard,
//     LicenseSourceDepsDevVersionSPDX, LicenseSourceProjectFallback, LicenseSourceDerivedFromVersion, LicenseSourceGitHubProjectSPDX)
//   - Raw: original upstream string prior to normalization (for debugging / traceability)
//   - IsSPDX: true when Identifier is a recognized SPDX license (and not NOASSERTION)
//
// Zero value (all empty/false) means no license detected.
type ResolvedLicense struct {
	Identifier string
	Source     string
	Raw        string
	IsSPDX     bool
}

// IsZero reports whether this license carries no information at all.
// A zero ResolvedLicense means we did not detect ANY project/version license data.
// Criteria:
//   - Identifier empty AND Source empty AND Raw empty AND IsSPDX == false
//
// Rationale: Using a method clarifies intent vs directly comparing struct fields everywhere.
func (r ResolvedLicense) IsZero() bool {
	return r.Identifier == "" && r.Source == "" && r.Raw == "" && !r.IsSPDX
}

// IsNonStandard reports whether the upstream provided license information, but it could not
// be mapped to a canonical SPDX identifier (non-SPDX / ambiguous / proprietary wording).
// It intentionally excludes the pure zero case (no data) and any successfully recognized SPDX.
// Detection rules:
//   - NOT IsZero (we have some data)
//   - !IsSPDX (normalization failed or SPDX list did not contain the value)
//   - Source is one of the known non-standard/raw indicators:
//   - LicenseSourceDepsDevProjectNonStandard
//   - LicenseSourceGitHubProjectNonStandard
//   - LicenseSourceDepsDevVersionRaw
//   - LicenseSourceGitHubVersionRaw (reserved / future)
//
// Notes:
//   - A promoted or fallback SPDX (derived-from-version / project-fallback) is NEVER non-standard.
//   - If at version level a raw string still normalized into a valid SPDX (IsSPDX=true) we treat it as standard.
func (r ResolvedLicense) IsNonStandard() bool {
	if r.IsZero() {
		return false
	}
	if r.IsSPDX { // recognized SPDX => standard
		return false
	}
	switch r.Source {
	case LicenseSourceDepsDevProjectNonStandard,
		LicenseSourceGitHubProjectNonStandard,
		LicenseSourceDepsDevVersionRaw,
		LicenseSourceGitHubVersionRaw:
		return true
	default:
		return false
	}
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
	// This is metadata-only (no automatic lifecycle assessment effect) and is propagated from deps.dev API Version.IsDeprecated.
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

// Score represents an individual scorecard score
type Score struct {
	Name       string
	Value      int
	Reason     string
	Details    []string
	ComputedAt time.Time
}

// CommitStats represents commit statistics
type CommitStats struct {
	Total       int
	BotCommits  int
	UserCommits int
	BotRatio    float64
	UserRatio   float64
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
