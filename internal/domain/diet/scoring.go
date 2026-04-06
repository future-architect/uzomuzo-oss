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

// ComputeImpactScore calculates the removability priority for a single dependency.
func ComputeImpactScore(graph GraphMetrics, coupling CouplingAnalysis, health HealthSignals, totalTransitive int) ImpactScore {
	graphImpact := normalizeGraphImpact(graph, totalTransitive)
	couplingEffort := normalizeCouplingEffort(coupling)
	healthRisk := health.HealthRisk

	priority := graphImpact * healthRisk * (1.0 - couplingEffort)
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
func ComputeSummary(entries []DietEntry) DietSummary {
	s := DietSummary{}
	for _, e := range entries {
		switch e.Relation {
		case RelationDirect:
			s.TotalDirect++
		case RelationTransitive:
			s.TotalTransitive++
		}
		s.TotalExclusiveTransitive += e.Graph.ExclusiveTransitiveCount
		if e.Coupling.IsUnused && e.Relation == RelationDirect {
			s.UnusedDirect++
		}
		if e.Scores.Difficulty == DifficultyTrivial || e.Scores.Difficulty == DifficultyEasy {
			if e.Scores.PriorityScore > 0.3 {
				s.EasyWins++
			}
		}
		if e.Scores.PriorityScore > 0.2 {
			s.EstimatedRemovable++
		}
	}
	return s
}

func normalizeGraphImpact(g GraphMetrics, totalTransitive int) float64 {
	if totalTransitive == 0 {
		return 0.1
	}
	raw := float64(g.ExclusiveTransitiveCount) / float64(totalTransitive)
	return math.Min(raw+0.1, 1.0)
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
	case effort == 0.0:
		return DifficultyTrivial
	case effort < 0.25:
		return DifficultyEasy
	case effort < 0.6:
		return DifficultyModerate
	default:
		return DifficultyHard
	}
}
