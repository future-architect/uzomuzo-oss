// Package audit defines verdict logic for dependency health auditing.
package audit

import (
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// Verdict represents the audit outcome for a single dependency.
type Verdict string

const (
	// VerdictOK indicates the dependency is actively maintained and healthy.
	VerdictOK Verdict = "ok"
	// VerdictCaution indicates the dependency shows warning signs (stalled, legacy, scheduled EOL).
	VerdictCaution Verdict = "caution"
	// VerdictReplace indicates the dependency is EOL and should be replaced.
	VerdictReplace Verdict = "replace"
	// VerdictReview indicates insufficient data to make a determination.
	VerdictReview Verdict = "review"
)

// DeriveVerdict computes a Verdict from an Analysis result.
// It takes the worst (most severe) of lifecycle and build integrity verdicts.
// This is a pure function with no I/O.
func DeriveVerdict(a *analysis.Analysis) Verdict {
	if a == nil {
		return VerdictReview
	}
	if a.Error != nil {
		return VerdictReview
	}
	return maxSeverity(deriveLifecycleVerdict(a), deriveBuildVerdict(a))
}

// deriveLifecycleVerdict computes verdict from lifecycle signals only.
func deriveLifecycleVerdict(a *analysis.Analysis) Verdict {
	if a.IsArchived() {
		return VerdictReplace
	}
	// EOL status from primary sources (catalog, registry) takes precedence over
	// lifecycle label. This is intentionally redundant with the label switch below:
	// if EOLStatus and MaintenanceStatus disagree (a data inconsistency), the
	// primary-source EOL signal wins.
	if a.EOL.IsEOL() {
		return VerdictReplace
	}
	if a.EOL.IsPlannedEOL() {
		return VerdictCaution
	}

	lr := a.GetLifecycleResult()
	if lr == nil {
		return VerdictReview
	}

	switch analysis.MaintenanceStatus(lr.Label) {
	case analysis.LabelActive, analysis.LabelLegacySafe:
		return VerdictOK
	case analysis.LabelStalled:
		return VerdictCaution
	case analysis.LabelEOLConfirmed, analysis.LabelEOLEffective:
		return VerdictReplace
	case analysis.LabelEOLScheduled:
		return VerdictCaution
	default:
		return VerdictReview
	}
}

// deriveBuildVerdict computes verdict from build integrity signals.
// Ungraded does not participate in verdict composition (returns VerdictOK)
// to prevent mass noise for packages without Scorecard coverage.
func deriveBuildVerdict(a *analysis.Analysis) Verdict {
	br := a.GetBuildHealthResult()
	if br == nil {
		return VerdictOK
	}
	switch analysis.BuildIntegrityLabel(br.Label) {
	case analysis.BuildLabelHardened:
		return VerdictOK
	case analysis.BuildLabelModerate:
		return VerdictCaution
	case analysis.BuildLabelWeak:
		return VerdictReplace
	case analysis.BuildLabelUngraded:
		return VerdictOK
	default:
		return VerdictOK
	}
}

// maxSeverity returns the more severe of two verdicts.
// Severity order: ok < review < caution < replace.
func maxSeverity(a, b Verdict) Verdict {
	if severityOrder(a) >= severityOrder(b) {
		return a
	}
	return b
}

func severityOrder(v Verdict) int {
	switch v {
	case VerdictOK:
		return 0
	case VerdictReview:
		return 1
	case VerdictCaution:
		return 2
	case VerdictReplace:
		return 3
	default:
		return 0
	}
}
