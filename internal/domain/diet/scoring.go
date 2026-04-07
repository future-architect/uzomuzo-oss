package diet

import (
	"math"
	"sort"
)

// Relation constants for DietEntry.Relation.
const (
	RelationDirect     = "direct"
	RelationTransitive = "transitive"
)

// Difficulty constants for ImpactScore.Difficulty.
const (
	DifficultyTrivial  = "trivial"
	DifficultyEasy     = "easy"
	DifficultyModerate = "moderate"
	DifficultyHard     = "hard"
)

// Summary classification thresholds for ComputeSummary.
const (
	// easyWinScoreThreshold is the minimum PriorityScore for a trivial/easy dep
	// to be counted as an "easy win" — high impact and low effort.
	easyWinScoreThreshold = 0.3
	// actionableScoreThreshold is the minimum PriorityScore for a dep
	// to be counted as "estimated removable" in the summary.
	actionableScoreThreshold = 0.2
)

// ComputeImpactScore calculates the removability priority for a single dependency.
// maxExclusive is the largest ExclusiveTransitiveCount across all entries in the dataset,
// used to normalize GraphImpact relative to the most impactful dependency.
func ComputeImpactScore(graph GraphMetrics, coupling CouplingAnalysis, health HealthSignals, maxExclusive int) ImpactScore {
	graphImpact := normalizeGraphImpact(graph, maxExclusive)
	couplingEffort := normalizeCouplingEffort(coupling)
	healthRisk := health.HealthRisk

	effortFactor := math.Max(1.0-couplingEffort, 0.05)
	priority := graphImpact * healthRisk * effortFactor
	// Unused dependencies are always high priority regardless of health.
	if coupling.IsUnused {
		priority = math.Max(priority, graphImpact*0.8)
	}

	return ImpactScore{
		GraphImpact:    graphImpact,
		CouplingEffort: couplingEffort,
		HealthRisk:     healthRisk,
		PriorityScore:  priority,
		Difficulty:     classifyDifficulty(couplingEffort),
	}
}

// RankEntries sorts entries by PriorityScore descending and assigns ranks.
func RankEntries(entries []DietEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Scores.PriorityScore > entries[j].Scores.PriorityScore
	})
	for i := range entries {
		entries[i].Scores.Rank = i + 1
	}
}

// ComputeSummary generates aggregate statistics from ranked entries.
// totalTransitive is the graph-level count of transitive dependencies (from GraphResult),
// since diet plan entries only contain direct dependencies.
func ComputeSummary(entries []DietEntry, totalTransitive int) DietSummary {
	s := DietSummary{
		TotalTransitive: totalTransitive,
	}
	for _, e := range entries {
		if e.Relation == RelationDirect {
			s.TotalDirect++
		}
		s.TotalExclusiveTransitive += e.Graph.ExclusiveTransitiveCount
		if e.Coupling.IsUnused && e.Relation == RelationDirect {
			s.UnusedDirect++
		}
		if e.Scores.Difficulty == DifficultyTrivial || e.Scores.Difficulty == DifficultyEasy {
			if e.Scores.PriorityScore > easyWinScoreThreshold {
				s.EasyWins++
			}
		}
		if e.Scores.PriorityScore > actionableScoreThreshold {
			s.EstimatedRemovable++
		}
		if e.Graph.StaysAsIndirect() {
			s.StaysAsIndirectCount++
		}
	}
	return s
}

func normalizeGraphImpact(g GraphMetrics, maxExclusive int) float64 {
	if maxExclusive == 0 {
		return 0.1
	}
	raw := float64(g.ExclusiveTransitiveCount) / float64(maxExclusive)
	// Scale to [0.1, 1.0] so even zero-exclusive deps retain a small base score.
	return math.Min(0.1+0.9*raw, 1.0)
}

func normalizeCouplingEffort(c CouplingAnalysis) float64 {
	if c.IsUnused {
		return 0.0
	}
	fileScore := logistic(float64(c.ImportFileCount), 5.0)
	callScore := logistic(float64(c.CallSiteCount), 20.0)
	apiScore := logistic(float64(c.APIBreadth), 10.0)
	return 0.4*fileScore + 0.4*callScore + 0.2*apiScore
}

func logistic(x, midpoint float64) float64 {
	k := 4.0 / midpoint
	return 1.0 / (1.0 + math.Exp(-k*(x-midpoint)))
}

func classifyDifficulty(effort float64) string {
	switch {
	case effort < 1e-9:
		return DifficultyTrivial
	case effort < 0.25:
		return DifficultyEasy
	case effort < 0.6:
		return DifficultyModerate
	default:
		return DifficultyHard
	}
}
