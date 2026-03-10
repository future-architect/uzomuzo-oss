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
}

// LatestAdvisories returns (count, advisories) for the "latest" version per priority order:
// Stable > MaxSemver > PreRelease > Requested. If Stable exists it is always chosen even if zero length.
func (ri *ReleaseInfo) LatestAdvisories() (int, []Advisory) {
	if ri == nil {
		return 0, nil
	}
	if ri.StableVersion != nil {
		return len(ri.StableVersion.Advisories), ri.StableVersion.Advisories
	}
	if ri.MaxSemverVersion != nil {
		return len(ri.MaxSemverVersion.Advisories), ri.MaxSemverVersion.Advisories
	}
	if ri.PreReleaseVersion != nil {
		return len(ri.PreReleaseVersion.Advisories), ri.PreReleaseVersion.Advisories
	}
	if ri.RequestedVersion != nil {
		return len(ri.RequestedVersion.Advisories), ri.RequestedVersion.Advisories
	}
	return 0, nil
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
}
