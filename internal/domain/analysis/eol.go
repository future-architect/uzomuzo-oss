// Package analysis contains domain models and lifecycle assessment inputs for EOL handling
package analysis

import (
	"context"
	"time"
)

// EOLState enumerates the final EOL outcome.
type EOLState string

const (
	// EOLUnknown means not evaluated yet.
	EOLUnknown EOLState = "Unknown"
	// EOLNotEOL indicates no EOL based on current evidence.
	EOLNotEOL EOLState = "NotEOL"
	// EOLEndOfLife indicates primary sources mark the package/project as EOL/abandoned/sunset.
	EOLEndOfLife EOLState = "EOL"
	// EOLScheduled indicates catalog has a future scheduled EOL date (not yet effective).
	EOLScheduled EOLState = "Scheduled"
)

// EOLEvidence describes an EOL-related primary evidence.
type EOLEvidence struct {
	// Source describes the origin (e.g., Packagist, Registry).
	Source string
	// Summary is a human-readable summary of the evidence.
	Summary string
	// Reference is a URL or identifier where the evidence can be verified.
	Reference string
	// Confidence is a 0..1 score indicating extraction confidence.
	Confidence float64
}

// EOLStatus aggregates final EOL decision and supporting evidence.
type EOLStatus struct {
	// Decision
	State EOLState

	// Optional successor identifier or URL
	Successor string

	// ScheduledAt is the future scheduled EOL date; aligns with catalog eol_date when present.
	ScheduledAt *time.Time

	// Reason is a short English human-reviewed rationale from the catalog (human judgment reason).
	// Empty if not provided by catalog or no human judgment applied.
	Reason string

	// ReasonJa is the Japanese translation (if available) of the human-reviewed rationale.
	// Present only when catalog contains reason_ja for the human judgment.
	ReasonJa string

	// Evidence list from various sources (catalog/registry/maven/nuget/readme etc.)
	Evidences []EOLEvidence
}

// FinalReason returns a single authoritative rationale string following a
// deterministic priority:
//  1. Human-reviewed catalog Reason field if non-empty.
//  2. Evidence with Source == "HumanCatalog" (only if Reason empty).
//  3. Evidence with highest Confidence (ties: first encountered).
//  4. Fallback: first evidence (if any exist).
//  5. Otherwise "".
//
// This keeps presentation layers simple and avoids relying on evidence append order
// beyond the explicit fallback step. Adjust priorities only here if future
// semantics evolve.
func (s EOLStatus) FinalReason() string {
	// 1. Catalog human-reviewed reason
	if s.Reason != "" {
		return s.Reason
	}
	if len(s.Evidences) == 0 {
		return ""
	}
	// Track HumanCatalog evidence & highest confidence
	var humanCat *EOLEvidence
	var best *EOLEvidence
	for i := range s.Evidences {
		ev := &s.Evidences[i]
		if ev.Source == "HumanCatalog" && humanCat == nil {
			humanCat = ev
		}
		if best == nil || ev.Confidence > best.Confidence {
			best = ev
		}
	}
	if humanCat != nil {
		return humanCat.Summary
	}
	if best != nil && best.Summary != "" {
		return best.Summary
	}
	return s.Evidences[0].Summary
}

// FinalReasonJa returns the Japanese rationale if available; otherwise the
// English FinalReason() value. Evidence summaries are currently English-only.
func (s EOLStatus) FinalReasonJa() string {
	if s.ReasonJa != "" {
		return s.ReasonJa
	}
	return s.FinalReason()
}

// IsEOL returns true if the status is a strong EOL decision (now EOL).
func (s EOLStatus) IsEOL() bool { return s.State == EOLEndOfLife }

// IsPlannedEOL returns true if the status indicates a planned (future) EOL.
func (s EOLStatus) IsPlannedEOL() bool { return s.State == EOLScheduled }

// HumanState returns a user-friendly string for the EOL state.
func (s EOLStatus) HumanState() string {
	switch s.State {
	case EOLEndOfLife:
		return "EOL"
	case EOLScheduled:
		return "Scheduled EOL"
	case EOLNotEOL:
		return "Not EOL"
	default:
		return "Unknown"
	}
}

// EOLEvaluatorPort defines how EOL status is evaluated from primary sources.
// Implementations live in the Infrastructure layer.
type EOLEvaluatorPort interface {
	// EvaluateBatch computes EOL status for analyses keyed by input key (PURL or repo URL).
	EvaluateBatch(ctx context.Context, analyses map[string]*Analysis) (map[string]EOLStatus, error)
}

