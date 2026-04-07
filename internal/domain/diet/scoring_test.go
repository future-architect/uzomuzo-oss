package diet

import "testing"

func TestComputeImpactScore_UnusedDep(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 100}
	coupling := CouplingAnalysis{IsUnused: true}
	health := HealthSignals{HealthRisk: 0.5}

	// maxExclusive=100: this dep IS the most impactful (exclusive=100)
	score := ComputeImpactScore(graph, coupling, health, 100)

	if score.CouplingEffort != 0.0 {
		t.Errorf("unused dep should have zero coupling effort, got %f", score.CouplingEffort)
	}
	if score.Difficulty != "trivial" {
		t.Errorf("unused dep should be trivial, got %s", score.Difficulty)
	}
	if score.PriorityScore <= 0 {
		t.Errorf("unused dep should have positive priority, got %f", score.PriorityScore)
	}
	// With graphImpact=1.0, unused boost = max(0.5*1.0, 1.0*0.8) = 0.8
	if score.PriorityScore < 0.8 {
		t.Errorf("unused dep with max exclusive should have high priority, got %f", score.PriorityScore)
	}
}

func TestComputeImpactScore_HeavilyCoupled(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 5}
	coupling := CouplingAnalysis{ImportFileCount: 50, CallSiteCount: 200, APIBreadth: 30}
	health := HealthSignals{HealthRisk: 0.8}

	// maxExclusive=100: this dep has modest exclusive count relative to max
	score := ComputeImpactScore(graph, coupling, health, 100)

	if score.Difficulty != "hard" {
		t.Errorf("heavily coupled dep should be hard, got %s", score.Difficulty)
	}
	if score.CouplingEffort < 0.6 {
		t.Errorf("heavily coupled dep should have high coupling effort, got %f", score.CouplingEffort)
	}
}

func TestComputeImpactScore_EasyWin(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 50}
	coupling := CouplingAnalysis{ImportFileCount: 1, CallSiteCount: 2, APIBreadth: 1}
	health := HealthSignals{HealthRisk: 0.9, IsEOL: true}

	// maxExclusive=100: this dep owns half the max exclusive count
	score := ComputeImpactScore(graph, coupling, health, 100)

	if score.Difficulty != "easy" {
		t.Errorf("easy win should be easy difficulty, got %s", score.Difficulty)
	}
	// graphImpact = 0.1 + 0.9*(50/100) = 0.55, healthRisk=0.9, low coupling
	// PriorityScore should comfortably exceed the 0.3 easy_wins threshold
	if score.PriorityScore < 0.3 {
		t.Errorf("easy win should exceed easy_wins threshold (0.3), got %f", score.PriorityScore)
	}
}

func TestRankEntries(t *testing.T) {
	entries := []DietEntry{
		{PURL: "low", Scores: ImpactScore{PriorityScore: 0.1}},
		{PURL: "high", Scores: ImpactScore{PriorityScore: 0.9}},
		{PURL: "mid", Scores: ImpactScore{PriorityScore: 0.5}},
	}
	RankEntries(entries)

	if entries[0].PURL != "high" || entries[0].Scores.Rank != 1 {
		t.Errorf("expected high first with rank 1, got %s rank %d", entries[0].PURL, entries[0].Scores.Rank)
	}
	if entries[1].PURL != "mid" || entries[1].Scores.Rank != 2 {
		t.Errorf("expected mid second with rank 2, got %s rank %d", entries[1].PURL, entries[1].Scores.Rank)
	}
	if entries[2].PURL != "low" || entries[2].Scores.Rank != 3 {
		t.Errorf("expected low third with rank 3, got %s rank %d", entries[2].PURL, entries[2].Scores.Rank)
	}
}

func TestClassifyDifficulty(t *testing.T) {
	tests := []struct {
		effort float64
		want   string
	}{
		{0.0, "trivial"},
		{0.1, "easy"},
		{0.24, "easy"},
		{0.25, "moderate"},
		{0.5, "moderate"},
		{0.6, "hard"},
		{0.9, "hard"},
	}
	for _, tt := range tests {
		got := classifyDifficulty(tt.effort)
		if got != tt.want {
			t.Errorf("classifyDifficulty(%f) = %s, want %s", tt.effort, got, tt.want)
		}
	}
}

func TestGraphResult_MaxExclusiveTransitiveCount(t *testing.T) {
	tests := []struct {
		name    string
		metrics map[string]*GraphMetrics
		want    int
	}{
		{"nil metrics", nil, 0},
		{"empty metrics", map[string]*GraphMetrics{}, 0},
		{"single entry", map[string]*GraphMetrics{"a": {ExclusiveTransitiveCount: 5}}, 5},
		{"multiple entries", map[string]*GraphMetrics{
			"a": {ExclusiveTransitiveCount: 5},
			"b": {ExclusiveTransitiveCount: 50},
			"c": {ExclusiveTransitiveCount: 10},
		}, 50},
		{"all zero", map[string]*GraphMetrics{
			"a": {ExclusiveTransitiveCount: 0},
			"b": {ExclusiveTransitiveCount: 0},
		}, 0},
		{"nil value in map", map[string]*GraphMetrics{
			"a": nil,
			"b": {ExclusiveTransitiveCount: 7},
		}, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gr := &GraphResult{Metrics: tt.metrics}
			got := gr.MaxExclusiveTransitiveCount()
			if got != tt.want {
				t.Errorf("MaxExclusiveTransitiveCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNormalizeGraphImpact(t *testing.T) {
	tests := []struct {
		name         string
		exclusive    int
		maxExclusive int
		want         float64
	}{
		{"zero exclusive, max=50", 0, 50, 0.1},
		{"max exclusive, max=50", 50, 50, 1.0},
		{"half exclusive, max=50", 25, 50, 0.55},
		{"small exclusive, max=50", 1, 50, 0.118},
		{"zero maxExclusive", 5, 0, 0.1},
		{"exclusive exceeds max (defensive clamp)", 100, 47, 1.0},
	}
	const tolerance = 0.001
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := GraphMetrics{ExclusiveTransitiveCount: tt.exclusive}
			got := normalizeGraphImpact(g, tt.maxExclusive)
			if got < tt.want-tolerance || got > tt.want+tolerance {
				t.Errorf("normalizeGraphImpact(exclusive=%d, max=%d) = %f, want %f (±%f)",
					tt.exclusive, tt.maxExclusive, got, tt.want, tolerance)
			}
		})
	}
}

func TestComputeImpactScore_LargeProject(t *testing.T) {
	// Verify that a dependency with the highest exclusive transitive count
	// gets the maximum graph impact when normalized by maxExclusive.
	// This should keep the score above the easy_wins threshold.
	graph := GraphMetrics{ExclusiveTransitiveCount: 47}
	coupling := CouplingAnalysis{IsUnused: true}
	health := HealthSignals{HealthRisk: 0.5}

	score := ComputeImpactScore(graph, coupling, health, 47)

	// graphImpact = 0.1 + 0.9*(47/47) = 1.0
	// unused boost: max(1.0*0.5*1.0, 1.0*0.8) = 0.8
	if score.PriorityScore < easyWinScoreThreshold {
		t.Errorf("large project top dep should exceed easy_wins threshold (%0.2f), got %f", easyWinScoreThreshold, score.PriorityScore)
	}
}

func TestComputeSummary_EasyWinsWithNewScoring(t *testing.T) {
	// Verify that EasyWins and EstimatedRemovable are non-zero when
	// entries have scores above thresholds (which the new normalization enables).
	entries := []DietEntry{
		{
			Relation: RelationDirect,
			Coupling: CouplingAnalysis{IsUnused: true},
			Scores:   ImpactScore{Difficulty: DifficultyTrivial, PriorityScore: 0.8},
		},
		{
			Relation: RelationDirect,
			Coupling: CouplingAnalysis{ImportFileCount: 1},
			Scores:   ImpactScore{Difficulty: DifficultyEasy, PriorityScore: 0.35},
		},
		{
			Relation: RelationDirect,
			Coupling: CouplingAnalysis{ImportFileCount: 5},
			Scores:   ImpactScore{Difficulty: DifficultyModerate, PriorityScore: 0.25},
		},
		{
			Relation: RelationDirect,
			Scores:   ImpactScore{Difficulty: DifficultyTrivial, PriorityScore: 0.08},
		},
	}

	summary := ComputeSummary(entries, 100)

	if summary.EasyWins != 2 {
		t.Errorf("EasyWins = %d, want 2 (trivial@0.8 + easy@0.35)", summary.EasyWins)
	}
	if summary.EstimatedRemovable != 3 {
		t.Errorf("EstimatedRemovable = %d, want 3 (scores > 0.2)", summary.EstimatedRemovable)
	}
}

func TestComputeSummary_StaysAsIndirectCount(t *testing.T) {
	entries := []DietEntry{
		{
			Relation: RelationDirect,
			Graph:    GraphMetrics{IndirectVia: []string{"pkg:golang/other@v1.0.0"}},
			Scores:   ImpactScore{Difficulty: DifficultyEasy},
		},
		{
			Relation: RelationDirect,
			Graph:    GraphMetrics{}, // no IndirectVia → StaysAsIndirect() = false
			Scores:   ImpactScore{Difficulty: DifficultyTrivial},
		},
		{
			Relation: RelationDirect,
			Graph:    GraphMetrics{IndirectVia: []string{"pkg:golang/a@v1.0.0", "pkg:golang/b@v1.0.0"}},
			Scores:   ImpactScore{Difficulty: DifficultyHard},
		},
	}

	summary := ComputeSummary(entries, 50)

	if summary.StaysAsIndirectCount != 2 {
		t.Errorf("StaysAsIndirectCount = %d, want 2", summary.StaysAsIndirectCount)
	}
	if summary.TotalDirect != 3 {
		t.Errorf("TotalDirect = %d, want 3", summary.TotalDirect)
	}
}
