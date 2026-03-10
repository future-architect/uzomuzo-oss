// Package analysis defines the core domain aggregates
package analysis

import (
	"strings"
	"time"

	"github.com/future-architect/uzomuzo/internal/common/purl"
)

// Analysis represents the complete analysis result using domain entities (Aggregate Root)
// Optimized for direct external usage without JSON serialization overhead
type Analysis struct {
	// ---------------------------------------------------------------------------
	// Identification fields (externally observable)
	// ---------------------------------------------------------------------------
	// OriginalPURL:
	//   - EXACT user / caller supplied identifier (or directly derived base when a non-PURL
	//     input such as a GitHub URL is first converted).
	//   - Never rewritten for stylistic normalization (case, qualifier ordering, etc.).
	//   - May be versionless even when a version is later resolved.
	//   - Purpose: auditability, faithful echo-back, reproducibility of the *request*.
	//   - Examples:
	//       Input: "pkg:npm/React"            => OriginalPURL = "pkg:npm/React"
	//       GitHub URL -> base:                => OriginalPURL = "pkg:golang/github.com/gin-gonic/gin"
	//       Collapsed Maven coords input:      => OriginalPURL = "pkg:maven:org.slf4j:slf4j-api@2.0.16"
	OriginalPURL string

	// EffectivePURL:
	//   - The *resolved* / *normalized for analysis* identifier actually used for downstream
	//     fetching, scoring and enrichment.
	//   - May add or replace a version (@1.2.3), expand collapsed coordinates, or apply
	//     ecosystem-specific canonical forms required by APIs (while preserving OriginalPURL).
	//   - If the user already supplied a fully qualified versioned PURL and no transformation
	//     is needed, it will equal OriginalPURL.
	//   - Examples continuing from above:
	//       Input: "pkg:npm/React" (latest resolved 18.3.1)  => EffectivePURL = "pkg:npm/react@18.3.1"
	//       GitHub URL base (stable v1.10.0)                 => EffectivePURL = "pkg:golang/github.com/gin-gonic/gin@v1.10.0"
	//       Collapsed Maven (expanded path)                  => EffectivePURL = "pkg:maven/org.slf4j/slf4j-api@2.0.16"
	EffectivePURL string

	// CanonicalKey:
	//   - Deterministic, lowercase, versionless key produced by purl.CanonicalKey.
	//   - Used ONLY for internal maps / dedup / catalog lookups (never display directly).
	//   - Computation preference order: OriginalPURL > EffectivePURL (first non-empty).
	//   - Safe to leave empty; callers should invoke EnsureCanonical() when needed.
	//   - Example for any of the above React variants => "pkg:npm/react".
	CanonicalKey string

	RepoURL string

	// Aggregate components
	Package    *Package
	Repository *Repository

	// Scores
	OverallScore float64
	Scores       map[string]*ScoreEntity

	// Repository state and activity
	RepoState   *RepoState
	CommitStats *CommitStats

	// Release information
	ReleaseInfo *ReleaseInfo

	// AxisResults stores per-axis assessment outputs (extensible: lifecycle, build_health, etc.)
	AxisResults map[AssessmentAxis]*AssessmentResult
	// Zero value of EOL means not evaluated or no evidence of EOL.
	EOL EOLStatus

	// DependentCount is the number of packages that depend on this package.
	// Zero means unknown or unsupported ecosystem.
	DependentCount int

	// Canonical package links (homepage, registry, docs, changelog)
	PackageLinks *PackageLinks

	// External references (populated from deps.dev project data)
	// Direct links to OpenSSF Scorecard viewer and API for this project
	ScorecardURL    string
	ScorecardAPIURL string

	// ProjectLicense provides repository-wide license info in unified struct form.
	// Zero value (Identifier=="" && Source=="") means no project-level license detected.
	// Non-standard placeholder from deps.dev: Identifier=="", Source==LicenseSourceDepsDevProjectNonStandard, Raw holds original.
	// Promotion from single version SPDX: Source==LicenseSourceDerivedFromVersion.
	ProjectLicense ResolvedLicense

	// RequestedVersionLicenses provides per-requested-version license information with source tracking.
	// Each ResolvedLicense captures:
	//   - Identifier: normalized SPDX identifier when recognized (canonical casing) OR best-effort normalized value
	//   - Source: origin of this license (e.g. LicenseSourceDepsDevVersionSPDX, LicenseSourceDepsDevVersionRaw, LicenseSourceProjectFallback)
	//   - Raw: original upstream string prior to normalization
	//   - IsSPDX: true when Identifier is a recognized SPDX identifier
	// Fallback semantics:
	//   - If version-specific probing yields zero usable licenses and ProjectLicense is set, we set a single
	//     entry sourced from project-fallback.
	//   - If all collected version licenses are non-SPDX while project license is a valid SPDX, we replace
	//     the slice with a single project-fallback entry (previously referred to as "harmonize").
	RequestedVersionLicenses []ResolvedLicense

	// Metadata
	AnalyzedAt time.Time
	Duration   time.Duration
	Error      error
}

// HasRecentStableRelease determines if there was a recent stable release within the given days
func (a *Analysis) HasRecentStableRelease(days int) bool {
	if a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil {
		return false
	}
	if a.ReleaseInfo.StableVersion.PublishedAt.IsZero() {
		return false
	}
	daysAgo := int(time.Since(a.ReleaseInfo.StableVersion.PublishedAt).Hours() / 24)
	return daysAgo <= days
}

// HasRecentPrereleaseRelease determines if there was a recent prerelease within the given days
func (a *Analysis) HasRecentPrereleaseRelease(days int) bool {
	if a.ReleaseInfo == nil || a.ReleaseInfo.PreReleaseVersion == nil {
		return false
	}
	if a.ReleaseInfo.PreReleaseVersion.PublishedAt.IsZero() {
		return false
	}
	daysAgo := int(time.Since(a.ReleaseInfo.PreReleaseVersion.PublishedAt).Hours() / 24)
	return daysAgo <= days
}

// HasRequestedVersionInfo determines if requested version information is available
func (a *Analysis) HasRequestedVersionInfo() bool {
	return a.ReleaseInfo != nil &&
		a.ReleaseInfo.RequestedVersion != nil &&
		a.ReleaseInfo.RequestedVersion.Version != ""
}

// HasRecentCommit checks if there's a recent commit within the given days
func (a *Analysis) HasRecentCommit(days int) bool {
	if a.RepoState == nil {
		return false
	}
	return a.GetDaysSinceLastCommit() <= days
}

// HasRecentHumanCommit checks if there's a recent human commit within the given days
func (a *Analysis) HasRecentHumanCommit(days int) bool {
	if a.RepoState == nil {
		return false
	}
	return a.GetDaysSinceLastHumanCommit() <= days
}

// GetDaysSinceLastCommit returns days since the last commit
func (a *Analysis) GetDaysSinceLastCommit() int {
	if a.RepoState == nil {
		return 9999 // Large number if no data
	}
	return a.RepoState.DaysSinceLastCommit
}

// GetDaysSinceLastHumanCommit returns days since the last human commit
func (a *Analysis) GetDaysSinceLastHumanCommit() int {
	if a.RepoState == nil || a.RepoState.LatestHumanCommit == nil {
		return 9999 // Large number if no data
	}
	days := int(time.Since(*a.RepoState.LatestHumanCommit).Hours() / 24)
	return days
}

// GetLastHumanCommitYears returns years since the last human commit
func (a *Analysis) GetLastHumanCommitYears() float64 {
	days := a.GetDaysSinceLastHumanCommit()
	if days == 9999 {
		return 999.0 // Large number if no data
	}
	return float64(days) / 365.0
}

// GetBotRatio gets the ratio of bot commits
func (a *Analysis) GetBotRatio() float64 {
	if a.RepoState == nil || a.RepoState.CommitStats == nil {
		return 0.0
	}
	return a.RepoState.CommitStats.BotRatio
}

// IsArchived returns whether the repository is archived
func (a *Analysis) IsArchived() bool {
	if a.RepoState == nil {
		return false
	}
	return a.RepoState.IsArchived
}

// IsDisabled returns whether the repository is disabled
func (a *Analysis) IsDisabled() bool {
	if a.RepoState == nil {
		return false
	}
	return a.RepoState.IsDisabled
}

// IsMaintenanceOk determines if the maintenance score is sufficient
func (a *Analysis) IsMaintenanceOk() bool {
	if a.Scores == nil {
		return false
	}
	if maintainedScore, exists := a.Scores["Maintained"]; exists {
		return float64(maintainedScore.Value()) >= 3.0 // MaintLow constant value
	}
	return false
}

// ============================================================================
// Scorecard data processing methods (migrated from types.Result)
// ============================================================================

// GetCheckMap converts each Scorecard check result to a name->score map
// Returns: Map with check names as keys and score values as values
func (a *Analysis) GetCheckMap() map[string]float64 {
	if len(a.Scores) == 0 {
		return make(map[string]float64)
	}

	m := make(map[string]float64, len(a.Scores))
	for name, scoreEntity := range a.Scores {
		m[name] = float64(scoreEntity.Value())
	}
	return m
}

// GetScore gets the score for a specified check name
// Args: name - check name
// Returns: score value (0-10), -1 if not found
func (a *Analysis) GetScore(name string) float64 {
	if scoreEntity, exists := a.Scores[name]; exists {
		return float64(scoreEntity.Value())
	}
	return -1
}

// HasError returns whether the analysis encountered an error
func (a *Analysis) HasError() bool {
	return a.Error != nil
}

// GetErrorMessage returns the error message if any
func (a *Analysis) GetErrorMessage() string {
	if a.Error != nil {
		return a.Error.Error()
	}
	return ""
}

// HasDeprecatedRequestedVersion reports whether the requested version is marked deprecated upstream.
// Returns false if no requested version info is available.
func (a *Analysis) HasDeprecatedRequestedVersion() bool {
	if a.ReleaseInfo == nil || a.ReleaseInfo.RequestedVersion == nil {
		return false
	}
	return a.ReleaseInfo.RequestedVersion.IsDeprecated
}

// TODO
// AllCandidateVersionsDeprecated reports whether both Stable and MaxSemver versions
// are marked deprecated upstream. This avoids false positives where only a single
// channel is deprecated (e.g., temporary yank) and should be combined with inactivity
// or primary EOL signals before driving a lifecycle EOL classification.

// CreateLifecycleAssessment creates a lifecycle assessment for this analysis using the domain assessor service
// This method encapsulates the lifecycle assessment creation logic within the domain layer
// GetLifecycleResult returns the lifecycle axis assessment if present.
func (a *Analysis) GetLifecycleResult() *AssessmentResult {
	if a == nil || a.AxisResults == nil {
		return nil
	}
	return a.AxisResults[LifecycleAxis]
}

// ============================================================================
// External API convenience methods (for Go package users)
// ============================================================================

// FinalLifecycleLabel derives a single high-level lifecycle label for UI/consumers
// prioritizing primary-source EOL signals, then lifecycle assessment.
// Order: EOL > Scheduled EOL > Lifecycle assessment label > Review Needed.
// FinalLifecycleLabel derives a single label with precedence: EOL > Scheduled EOL > lifecycle axis > Review Needed.
func (a *Analysis) FinalLifecycleLabel() string {
	if a == nil {
		return "Review Needed"
	}
	if a.EOL.IsEOL() {
		return "EOL"
	}
	if a.EOL.IsPlannedEOL() {
		return "Scheduled EOL"
	}
	if lr := a.GetLifecycleResult(); lr != nil {
		return string(lr.Label)
	}
	return "Review Needed"
}

// DisplayPURL returns the most user-meaningful PURL for presentation (original if available, otherwise effective).
func (a *Analysis) DisplayPURL() string {
	if a == nil {
		return ""
	}
	if a.OriginalPURL != "" {
		return a.OriginalPURL
	}
	return a.EffectivePURL
}

// IsVersionResolved reports whether EffectivePURL includes a @version segment.
func (a *Analysis) IsVersionResolved() bool {
	if a == nil || a.EffectivePURL == "" {
		return false
	}
	return strings.Contains(a.EffectivePURL, "@")
}

// EnsureCanonical populates CanonicalKey if empty using the OriginalPURL (preferring it) or EffectivePURL.
func (a *Analysis) EnsureCanonical() {
	if a == nil || a.CanonicalKey != "" {
		return
	}
	raw := a.OriginalPURL
	if raw == "" {
		raw = a.EffectivePURL
	}
	if raw == "" {
		return
	}
	a.CanonicalKey = purl.CanonicalKey(raw)
}
