package audit

import (
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
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
	// Via is the direct parent action URL that caused this transitive dependency to be discovered.
	// Only populated when Source is SourceActionsTransitive.
	Via string
	// Relation indicates whether this dependency is direct, transitive, or unknown
	// relative to the user's project. Populated when input is an SBOM or go.mod.
	Relation depparser.DependencyRelation
	// ViaParents lists short names of direct dependencies through which this
	// transitive dependency is pulled in. Populated when input is an SBOM.
	ViaParents []string
}
