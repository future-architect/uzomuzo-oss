package diet

// Scope constants for DietEntry.Scope.
const (
	// ScopeTool indicates a Go tool directive dependency (Go 1.24+).
	// Tool deps are dev/CI executables that intentionally have zero source imports.
	ScopeTool = "tool"

	// ScopeRuntime indicates a dependency loaded via runtime mechanisms
	// (reflection, ServiceLoader, classpath resources) rather than static imports.
	// Examples: JDBC drivers, logging backends, Spring auto-config, webjars.
	// These deps intentionally have zero source-level imports and should not
	// be flagged as unused.
	ScopeRuntime = "runtime"
)

// DietEntry represents a single dependency's removability analysis.
type DietEntry struct {
	PURL      string
	Name      string
	Ecosystem string
	Version   string
	Relation  string // "direct" or "transitive"
	Scope     string // "" (default), "tool" (Go tool directive), or "runtime" (loaded via runtime mechanisms such as reflection, ServiceLoader, or classpath resources)

	Graph    GraphMetrics
	Coupling CouplingAnalysis
	Health   HealthSignals
	Scores   ImpactScore
}

// GraphResult contains the full graph analysis output.
type GraphResult struct {
	DirectDeps      []string
	AllDeps         []string
	Metrics         map[string]*GraphMetrics
	TotalTransitive int
}

// MaxExclusiveTransitiveCount returns the largest ExclusiveTransitiveCount
// across all metrics in the result. Returns 0 if there are no metrics.
func (r *GraphResult) MaxExclusiveTransitiveCount() int {
	if r == nil {
		return 0
	}
	m := 0
	for _, gm := range r.Metrics {
		if gm != nil && gm.ExclusiveTransitiveCount > m {
			m = gm.ExclusiveTransitiveCount
		}
	}
	return m
}

// GraphMetrics captures dependency graph impact for a single direct dependency.
type GraphMetrics struct {
	ExclusiveTransitiveCount int
	TotalTransitiveCount     int
	SharedTransitiveCount    int
	IndirectVia              []string // PURLs of direct deps that transitively depend on this one
}

// StaysAsIndirect returns true if removing this direct dep leaves it reachable
// via another direct dep (i.e., it would remain as an indirect dependency).
func (g GraphMetrics) StaysAsIndirect() bool {
	return len(g.IndirectVia) > 0
}

// CouplingAnalysis captures how deeply a dependency is wired into the codebase.
type CouplingAnalysis struct {
	ImportFileCount int
	CallSiteCount   int
	APIBreadth      int
	ImportFiles     []string
	Symbols         []string
	IsUnused        bool

	// Import style flags — affect call-site tracking accuracy.
	// HasBlankImport indicates side-effect-oriented or feature-detection import patterns.
	// Examples: Go: import _ "pkg", JS: import 'x', CJS: require('x'),
	// Python: try/except import checks. This flag can co-exist with callable API
	// usage elsewhere and should not be interpreted as implying zero coupling effort.
	HasBlankImport    bool
	HasDotImport      bool // Go: import . "pkg" (symbols callable without prefix — undercounted)
	HasWildcardImport bool // Python: from x import * / Java: import static x.* (undercounted)
}

// HealthSignals captures upstream health factors relevant to removability priority.
type HealthSignals struct {
	MaintenanceStatus  string
	IsEOL              bool
	IsStalled          bool
	HasVulnerabilities bool
	VulnerabilityCount int
	MaxCVSSScore       float64
	OverallScore       float64
	HealthRisk         float64
}

// ImpactScore contains the final computed scores for a dependency.
type ImpactScore struct {
	GraphImpact    float64
	CouplingEffort float64
	HealthRisk     float64
	PriorityScore  float64
	Rank           int
	Difficulty     string
}
