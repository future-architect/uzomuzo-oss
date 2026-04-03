package audit

import (
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// EntrySource identifies how a dependency was discovered.
type EntrySource string

const (
	// SourceDirect means the user provided this dependency directly (default).
	SourceDirect EntrySource = ""
	// SourceActions means this dependency was discovered from a GitHub Actions workflow.
	SourceActions EntrySource = "actions"
	// SourceActionsTransitive means this dependency was discovered as a transitive
	// composite action dependency (an action used by another action).
	SourceActionsTransitive EntrySource = "actions-transitive"
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
	// Source indicates how this entry was discovered (empty = direct input, "actions" = from workflow).
	Source EntrySource
}
