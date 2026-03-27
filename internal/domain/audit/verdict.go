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
// This is a pure function with no I/O.
//
// Mapping:
//   - nil / error / archived              → review or replace
//   - EOL (Confirmed/Effective) or archived → replace
//   - Stalled or EOL-Scheduled            → caution
//   - Active or Legacy-Safe               → ok
//   - Anything else                        → review
func DeriveVerdict(a *analysis.Analysis) Verdict {
	if a == nil {
		return VerdictReview
	}
	if a.Error != nil {
		return VerdictReview
	}
	if a.IsArchived() {
		return VerdictReplace
	}
	// EOL status from primary sources (catalog, registry) takes precedence over
	// lifecycle label. This is intentionally redundant with the label switch below:
	// if EOLStatus and LifecycleLabel disagree (a data inconsistency), the
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

	// Lifecycle label from the assessment axis. EOL labels here are a secondary
	// signal (e.g., inferred from inactivity) and may overlap with the EOL checks
	// above — that overlap is intentional for defense-in-depth.
	switch lr.Label {
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
