package diet

import "testing"

func TestComputeImpactScore_UnusedDep(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 100, TotalTransitiveCount: 100}
	coupling := CouplingAnalysis{IsUnused: true}
	health := HealthSignals{HealthRisk: 0.5}

	score := ComputeImpactScore(graph, coupling, health, 200)

	if score.CouplingEffort != 0.0 {
		t.Errorf("unused dep should have zero coupling effort, got %f", score.CouplingEffort)
	}
	if score.Difficulty != "trivial" {
		t.Errorf("unused dep should be trivial, got %s", score.Difficulty)
	}
	if score.PriorityScore <= 0 {
		t.Errorf("unused dep should have positive priority, got %f", score.PriorityScore)
	}
}

func TestComputeImpactScore_HeavilyCoupled(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 5, TotalTransitiveCount: 5}
	coupling := CouplingAnalysis{ImportFileCount: 50, CallSiteCount: 200, APIBreadth: 30}
	health := HealthSignals{HealthRisk: 0.8}

	score := ComputeImpactScore(graph, coupling, health, 200)

	if score.Difficulty != "hard" {
		t.Errorf("heavily coupled dep should be hard, got %s", score.Difficulty)
	}
	if score.CouplingEffort < 0.6 {
		t.Errorf("heavily coupled dep should have high coupling effort, got %f", score.CouplingEffort)
	}
}

func TestComputeImpactScore_EasyWin(t *testing.T) {
	graph := GraphMetrics{ExclusiveTransitiveCount: 50, TotalTransitiveCount: 50}
	coupling := CouplingAnalysis{ImportFileCount: 1, CallSiteCount: 2, APIBreadth: 1}
	health := HealthSignals{HealthRisk: 0.9, IsEOL: true}

	score := ComputeImpactScore(graph, coupling, health, 200)

	if score.Difficulty != "easy" {
		t.Errorf("easy win should be easy difficulty, got %s", score.Difficulty)
	}
	if score.PriorityScore < 0.2 {
		t.Errorf("easy win should have reasonable priority, got %f", score.PriorityScore)
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
