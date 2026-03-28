// Package uzomuzo re-exports key domain types for external use.
// This provides access to the rich domain model without requiring
// knowledge of internal package structure.
package uzomuzo

// Re-export domain types & constants for external use.
// This file intentionally contains ONLY aliases and constant passthroughs – no logic –
// to keep the public surface stable while allowing internal iterative changes.
//
// DDD Note: The domain model lives in internal/domain. We expose a curated façade here
// without leaking internal package paths to callers.

import (
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// =============================
// Type Aliases (public façade)
// =============================

// Analysis represents the complete security analysis result (aggregate root).
type Analysis = domain.Analysis

// Package represents a software package with its ecosystem & version info.
type Package = domain.Package

// ScoreEntity represents a single Scorecard check result.
type ScoreEntity = domain.ScoreEntity

// AssessmentResult represents a single axis assessment result (currently lifecycle axis is implemented).
type AssessmentResult = domain.AssessmentResult

// RepoState contains repository activity & archive/disable flags.
type RepoState = domain.RepoState

// ReleaseInfo contains prioritized release channel/version metadata.
type ReleaseInfo = domain.ReleaseInfo

// VersionDetail represents a specific release channel version.
type VersionDetail = domain.VersionDetail

// Advisory represents a security advisory affecting a version.
type Advisory = domain.Advisory

// Repository represents a code repository being analyzed.
type Repository = domain.Repository

// PackageLinks holds canonical project links.
type PackageLinks = domain.PackageLinks

// CommitStats represents commit statistics (total, bot, user counts and ratios).
type CommitStats = domain.CommitStats

// ResolvedLicense represents normalized license information.
type ResolvedLicense = domain.ResolvedLicense

// NewScoreEntity creates a new ScoreEntity with the given name, value, maxValue, and reason.
var NewScoreEntity = domain.NewScoreEntity

// EOLStatus aggregates primary-source EOL evaluation + evidences.
type EOLStatus = domain.EOLStatus

// EOLEvidence captures a single EOL evidence item.
type EOLEvidence = domain.EOLEvidence

// MaintenanceStatus enumerates maintenance status labels.
type MaintenanceStatus = domain.MaintenanceStatus

// Deprecated: Use MaintenanceStatus instead.
type LifecycleLabel = MaintenanceStatus

// EOLState enumerates the primary-source EOL decision state.
type EOLState = domain.EOLState

// =============================
// Maintenance Status Labels (constants)
// =============================
const (
	LabelActive       MaintenanceStatus = domain.LabelActive
	LabelStalled      MaintenanceStatus = domain.LabelStalled
	LabelLegacySafe   MaintenanceStatus = domain.LabelLegacySafe
	LabelEOLConfirmed MaintenanceStatus = domain.LabelEOLConfirmed
	LabelEOLEffective MaintenanceStatus = domain.LabelEOLEffective
	LabelEOLScheduled MaintenanceStatus = domain.LabelEOLScheduled
	LabelReviewNeeded MaintenanceStatus = domain.LabelReviewNeeded
)

// =============================
// EOL States (constants)
// =============================
const (
	EOLUnknown   EOLState = domain.EOLUnknown
	EOLNotEOL    EOLState = domain.EOLNotEOL
	EOLEndOfLife EOLState = domain.EOLEndOfLife
	EOLScheduled EOLState = domain.EOLScheduled
)

// =============================
// Helper Accessors
// =============================

// FinalMaintenanceStatus returns the final single maintenance status (wrapper over Analysis.FinalMaintenanceStatus).
// Prefer this over directly inspecting LifecycleAssessment/EOL for simple UI decisions.
func FinalMaintenanceStatus(a *Analysis) string { return a.FinalMaintenanceStatus() }

// Deprecated: Use FinalMaintenanceStatus instead.
func FinalLifecycleLabel(a *Analysis) string { return FinalMaintenanceStatus(a) }

// LifecycleSummary provides a consolidated snapshot combining lifecycle assessment + primary-source EOL.
// This decouples callers from internal domain structs while giving richer context.
type LifecycleSummary struct {
	FinalLabel        string        // priority-ordered final label (EOL > Scheduled EOL > LifecycleAssessment > Review Needed)
	MaintenanceStatus string        // raw lifecycle assessment label (may be empty)
	LifecycleReason string        // rationale for lifecycle assessment
	EOLState        string        // raw EOL state (Unknown / NotEOL / EOL / Planned)
	EOLHumanState   string        // human-friendly EOL state label
	Successor       string        // successor project reference (when available)
	ScheduledAt     *time.Time    // scheduled EOL date (for scheduled state)
	EOLEvidences    []EOLEvidence // evidence list (may be empty)
	EOLReason       string        // catalog human judgment reason (English)
	EOLReasonJa     string        // catalog human judgment reason Japanese (if available)
}

// BuildLifecycleSummary constructs a LifecycleSummary from an Analysis.
// Safe for nil input.
func BuildLifecycleSummary(a *Analysis) LifecycleSummary {
	if a == nil {
		return LifecycleSummary{FinalLabel: "Review Needed", EOLState: string(EOLUnknown), EOLHumanState: "Unknown"}
	}
	ls := LifecycleSummary{FinalLabel: a.FinalMaintenanceStatus(), EOLState: string(a.EOL.State), EOLHumanState: a.EOL.HumanState(), Successor: a.EOL.Successor, ScheduledAt: a.EOL.ScheduledAt}
	ls.EOLReason = a.EOL.Reason
	ls.EOLReasonJa = a.EOL.ReasonJa
	if a.EOL.Evidences != nil {
		ls.EOLEvidences = make([]EOLEvidence, len(a.EOL.Evidences))
		copy(ls.EOLEvidences, a.EOL.Evidences)
	}
	if lr := a.GetLifecycleResult(); lr != nil {
		ls.MaintenanceStatus = string(lr.Label)
		ls.LifecycleReason = lr.Reason
	}
	return ls
}
