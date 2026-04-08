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

// Unused-dependency additive scoring coefficients.
// unusedBase = unusedGraphWeight*graphImpact + unusedHealthWeight*healthRisk + unusedBaseOffset
const (
	unusedGraphWeight  = 0.3
	unusedHealthWeight = 0.3
	unusedBaseOffset   = 0.2
)

// lifecycleScoreFloor is the minimum PriorityScore for dependencies with EOL
// or Archived lifecycle status. The multiplicative formula can produce
// near-zero scores for EOL deps with hard difficulty (high coupling effort),
// burying strategically important items at the bottom of the ranking. This
// floor ensures they remain visible and actionable. See #214.
const lifecycleScoreFloor = 0.10

// maintenanceStatusArchived is the HealthSignals.MaintenanceStatus value
// for GitHub-archived repositories. Defined here because the diet domain
// uses a plain string (not the analysis.MaintenanceStatus type).
const maintenanceStatusArchived = "Archived"

// ComputeImpactScore calculates the removability priority for a single dependency.
// maxExclusive is the largest ExclusiveTransitiveCount across all entries in the dataset,
// used to normalize GraphImpact relative to the most impactful dependency.
func ComputeImpactScore(graph GraphMetrics, coupling CouplingAnalysis, health HealthSignals, maxExclusive int) ImpactScore {
	graphImpact := normalizeGraphImpact(graph, maxExclusive)
	couplingEffort := normalizeCouplingEffort(coupling)
	healthRisk := health.HealthRisk

	effortFactor := math.Max(1.0-couplingEffort, 0.05)
	priority := graphImpact * healthRisk * effortFactor
	// Unused dependencies get an additive score: effort is zero so priority
	// should reflect the removal value (graph cleanup + health risk reduction).
	// The additive formula ensures even zero-exclusive unused deps can exceed
	// the easy_wins threshold (0.3), whereas the multiplicative formula
	// systematically suppresses scores when any sub-score is small.
	if coupling.IsUnused {
		unusedBase := unusedGraphWeight*graphImpact + unusedHealthWeight*healthRisk + unusedBaseOffset
		priority = math.Max(priority, unusedBase)
	}

	// EOL/Archived dependencies must never score below the floor. The
	// multiplicative formula heavily penalizes hard difficulty, which can
	// zero out the score for deeply coupled EOL deps — exactly the items
	// that most need strategic attention.
	if (health.IsEOL || health.MaintenanceStatus == maintenanceStatusArchived) && priority < lifecycleScoreFloor {
		priority = lifecycleScoreFloor
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
	// When all coupling counts are zero and IsUnused is false, no coupling
	// data is available, typically because source analysis was not performed.
	// Treat this as zero effort so that the difficulty label ("trivial") is
	// consistent regardless of whether --source was provided. Without this
	// guard, logistic(0, midpoint) returns ~0.018, which classifies as
	// "easy" instead of "trivial".
	if c.ImportFileCount == 0 && c.CallSiteCount == 0 && c.APIBreadth == 0 {
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
