package audit

import (
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// AuditEntry pairs a dependency's PURL with its analysis result and derived verdict.
type AuditEntry struct {
	// PURL is the Package URL used for evaluation.
	PURL string
	// Analysis is the full evaluation result (nil if evaluation failed entirely).
	Analysis *analysis.Analysis
	// Verdict is the derived audit outcome.
	Verdict Verdict
	// ErrorMsg is non-empty if the analysis encountered an error.
	ErrorMsg string
}
