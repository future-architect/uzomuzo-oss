package analysis

import (
	"context"
	"testing"
)

func TestBuildHealthAssessorService_Assess(t *testing.T) {
	tests := []struct {
		name      string
		scores    map[string]*ScoreEntity
		slsa      bool
		attest    bool
		wantLabel string
		wantScore string // expected Meta["score"], "-1" for ungraded
	}{
		{
			name: "all_signals_high",
			scores: map[string]*ScoreEntity{
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 10, 10, ""),
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 9, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 8, 10, ""),
				"Token-Permissions":  NewScoreEntity("Token-Permissions", 9, 10, ""),
				"Binary-Artifacts":   NewScoreEntity("Binary-Artifacts", 10, 10, ""),
				"Signed-Releases":    NewScoreEntity("Signed-Releases", 8, 10, ""),
				"Packaging":          NewScoreEntity("Packaging", 8, 10, ""),
				"Pinned-Dependencies": NewScoreEntity("Pinned-Dependencies", 7, 10, ""),
			},
			slsa:      true,
			attest:    true,
			wantLabel: string(BuildLabelHardened),
		},
		{
			name: "all_signals_low",
			scores: map[string]*ScoreEntity{
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 0, 10, ""),
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 0, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 0, 10, ""),
				"Token-Permissions":  NewScoreEntity("Token-Permissions", 0, 10, ""),
				"Binary-Artifacts":   NewScoreEntity("Binary-Artifacts", 0, 10, ""),
				"Signed-Releases":    NewScoreEntity("Signed-Releases", 0, 10, ""),
				"Packaging":          NewScoreEntity("Packaging", 0, 10, ""),
				"Pinned-Dependencies": NewScoreEntity("Pinned-Dependencies", 0, 10, ""),
			},
			wantLabel: string(BuildLabelWeak),
			wantScore: "0.0",
		},
		{
			name:      "no_signals_ungraded",
			scores:    map[string]*ScoreEntity{},
			wantLabel: string(BuildLabelUngraded),
			wantScore: "-1",
		},
		{
			name:      "nil_scores_ungraded",
			scores:    nil,
			wantLabel: string(BuildLabelUngraded),
			wantScore: "-1",
		},
		{
			name: "partial_signals_moderate",
			scores: map[string]*ScoreEntity{
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 5, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 6, 10, ""),
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 8, 10, ""),
			},
			wantLabel: string(BuildLabelModerate),
		},
		{
			name:      "slsa_only_ungraded",
			scores:    map[string]*ScoreEntity{},
			slsa:      true,
			wantLabel: string(BuildLabelUngraded),
			wantScore: "-1", // 1 signal < minEvaluatedSignals (3)
		},
		{
			name: "score_zero_vs_absent",
			scores: map[string]*ScoreEntity{
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 0, 10, ""),
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 0, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 0, 10, ""),
			},
			wantLabel: string(BuildLabelWeak),
			wantScore: "0.0",
		},
		{
			name: "below_min_signals_ungraded",
			scores: map[string]*ScoreEntity{
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", -1, 10, "inconclusive"),
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 8, 10, ""),
			},
			wantLabel: string(BuildLabelUngraded),
			wantScore: "-1", // 1 evaluated < minEvaluatedSignals (3)
		},
		{
			name: "inconclusive_check_excluded",
			scores: map[string]*ScoreEntity{
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", -1, 10, "inconclusive"),
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 8, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 9, 10, ""),
				"Token-Permissions":  NewScoreEntity("Token-Permissions", 7, 10, ""),
			},
			wantLabel: string(BuildLabelHardened),
			wantScore: "8.0", // 3 evaluated, Dangerous-Workflow excluded
		},
		{
			name: "example_from_adr",
			scores: map[string]*ScoreEntity{
				"Branch-Protection":  NewScoreEntity("Branch-Protection", 8, 10, ""),
				"Code-Review":        NewScoreEntity("Code-Review", 7, 10, ""),
				"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 10, 10, ""),
				"Pinned-Dependencies": NewScoreEntity("Pinned-Dependencies", 3, 10, ""),
			},
			// score = (7.5*8 + 7.5*7 + 10*10 + 5*3) / (7.5+7.5+10+5) = 227.5/30 = 7.58
			wantLabel: string(BuildLabelHardened),
			wantScore: "7.6", // rounded to 1 decimal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewBuildHealthAssessorService()
			a := &Analysis{
				SLSAVerified:        tt.slsa,
				AttestationVerified: tt.attest,
			}
			in := AssessmentInput{
				Analysis: a,
				Scores:   tt.scores,
			}
			res, err := svc.Assess(context.Background(), in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected non-nil result")
			}
			if res.Label != tt.wantLabel {
				t.Errorf("label = %q, want %q", res.Label, tt.wantLabel)
			}
			if tt.wantScore != "" {
				gotScore := res.Meta["score"]
				if gotScore != tt.wantScore {
					t.Errorf("score = %q, want %q", gotScore, tt.wantScore)
				}
			}
			if res.Axis != BuildHealthAxis {
				t.Errorf("axis = %q, want %q", res.Axis, BuildHealthAxis)
			}
		})
	}
}

func TestBuildHealthAssessorService_SignalCount(t *testing.T) {
	svc := NewBuildHealthAssessorService()
	scores := map[string]*ScoreEntity{
		"Branch-Protection":  NewScoreEntity("Branch-Protection", 5, 10, ""),
		"Code-Review":        NewScoreEntity("Code-Review", 6, 10, ""),
		"Dangerous-Workflow": NewScoreEntity("Dangerous-Workflow", 8, 10, ""),
	}
	res, err := svc.Assess(context.Background(), AssessmentInput{
		Analysis: &Analysis{},
		Scores:   scores,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have 10 signals total (3 used + 7 absent)
	if len(res.Signals) != 10 {
		t.Errorf("signal count = %d, want 10", len(res.Signals))
	}
	usedCount := 0
	for _, s := range res.Signals {
		if s.Role == SignalUsed {
			usedCount++
		}
	}
	if usedCount != 3 {
		t.Errorf("used signal count = %d, want 3", usedCount)
	}
}
