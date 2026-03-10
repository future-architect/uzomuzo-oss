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

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
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

// EOLStatus aggregates primary-source EOL evaluation + evidences.
type EOLStatus = domain.EOLStatus

// EOLEvidence captures a single EOL evidence item.
type EOLEvidence = domain.EOLEvidence

// LifecycleLabel enumerates lifecycle labels.
type LifecycleLabel = domain.LifecycleLabel

// EOLState enumerates the primary-source EOL decision state.
type EOLState = domain.EOLState

// =============================
// Lifecycle Labels (constants)
// =============================
const (
	LabelActive       LifecycleLabel = domain.LabelActive
	LabelStalled      LifecycleLabel = domain.LabelStalled
	LabelLegacySafe   LifecycleLabel = domain.LabelLegacySafe
	LabelEOLConfirmed LifecycleLabel = domain.LabelEOLConfirmed
	LabelEOLEffective LifecycleLabel = domain.LabelEOLEffective
	LabelEOLScheduled LifecycleLabel = domain.LabelEOLScheduled
	LabelReviewNeeded LifecycleLabel = domain.LabelReviewNeeded
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

// FinalLifecycleLabel returns the final single lifecycle label (wrapper over Analysis.FinalLifecycleLabel).
// Prefer this over directly inspecting LifecycleAssessment/EOL for simple UI decisions.
func FinalLifecycleLabel(a *Analysis) string { return a.FinalLifecycleLabel() }

// LifecycleSummary provides a consolidated snapshot combining lifecycle assessment + primary-source EOL.
// This decouples callers from internal domain structs while giving richer context.
type LifecycleSummary struct {
	FinalLabel      string        // priority-ordered final label (EOL > Scheduled EOL > LifecycleAssessment > Review Needed)
	LifecycleLabel  string        // raw lifecycle assessment label (may be empty)
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
	ls := LifecycleSummary{FinalLabel: a.FinalLifecycleLabel(), EOLState: string(a.EOL.State), EOLHumanState: a.EOL.HumanState(), Successor: a.EOL.Successor, ScheduledAt: a.EOL.ScheduledAt}
	ls.EOLReason = a.EOL.Reason
	ls.EOLReasonJa = a.EOL.ReasonJa
	if a.EOL.Evidences != nil {
		ls.EOLEvidences = make([]EOLEvidence, len(a.EOL.Evidences))
		copy(ls.EOLEvidences, a.EOL.Evidences)
	}
	if lr := a.GetLifecycleResult(); lr != nil {
		ls.LifecycleLabel = string(lr.Label)
		ls.LifecycleReason = lr.Reason
	}
	return ls
}
