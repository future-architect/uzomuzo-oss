package diet

// DietEntry represents a single dependency's removability analysis.
type DietEntry struct {
	PURL      string
	Name      string
	Ecosystem string
	Version   string
	Relation  string // "direct" or "transitive"

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
	m := 0
	for _, gm := range r.Metrics {
		if gm.ExclusiveTransitiveCount > m {
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
	IsUnused        bool
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
